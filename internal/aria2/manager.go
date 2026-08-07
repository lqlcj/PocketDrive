package aria2

import (
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"pocketdrive/internal/db"
	"pocketdrive/internal/httpx"
)

// Manager keeps aria2 task state mirrored in SQLite so history survives
// aria2 restarts, and handles the magnet metadata → real-download gid
// migration (followedBy).
type Manager struct {
	db       *gorm.DB
	c        *Client
	dataRoot string // data dir as seen by the aria2 process
	localDir string // 同一个网盘目录在 PocketDrive 自己进程里的路径
	degraded atomic.Bool
	// 全局设置是否已成功推给 aria2(重连后需要重推)
	globalsApplied atomic.Bool
	// Tracker 自动更新使用多个固定可信源，测试时可替换成本地 HTTP 服务。
	trackerSources []string
	// 自动更新、手动更新和保存设置后的补更新可能同时触发；串行化后
	// 再检查缓存时间，避免重复请求公网源和交错写入缓存。
	trackerUpdateMu sync.Mutex

	mu     sync.Mutex
	speeds map[string]int64

	// migMu 串行化「磁力元数据 gid → 真实下载 gid」的 followedBy 迁移，
	// 并避免迁移与同步错误处理并发时把旧记录重新写回来。
	migMu sync.Mutex

	verMu   sync.Mutex
	version string
	verAt   time.Time

	stop chan struct{}
}

// Version 返回 aria2 版本号(缓存 10 分钟);不可达时返回空串。
// aria2 跑在 sidecar 容器里,升级靠 docker compose pull,这里只做展示。
func (m *Manager) Version() string {
	m.verMu.Lock()
	defer m.verMu.Unlock()
	if m.version != "" && time.Since(m.verAt) < 10*time.Minute {
		return m.version
	}
	v, err := m.c.GetVersion()
	if err != nil {
		return ""
	}
	m.version, m.verAt = v, time.Now()
	return v
}

// Degraded 报告 aria2 当前是否不可达。
func (m *Manager) Degraded() bool { return m.degraded.Load() }

// NewManager:dataRoot 是 aria2 进程眼里的网盘路径,localDir 是同一个
// 目录在本进程眼里的路径(单容器/本机开发时两者相同)。
func NewManager(gdb *gorm.DB, c *Client, dataRoot, localDir string) *Manager {
	return &Manager{
		db:             gdb,
		c:              c,
		dataRoot:       dataRoot,
		localDir:       localDir,
		speeds:         make(map[string]int64),
		trackerSources: append([]string(nil), defaultTrackerSources...),
		stop:           make(chan struct{}),
	}
}

func (m *Manager) Start() {
	go func() {
		// 启动即探测一次,让前端在无任务时也能显示 aria2 连接状态
		_, err := m.c.GetVersion()
		m.degraded.Store(err != nil)
		if err == nil {
			m.applyGlobals()
		}

		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-m.stop:
				return
			case <-t.C:
				m.sync()
				// aria2 恢复可达后重推全局设置
				if !m.degraded.Load() && !m.globalsApplied.Load() {
					m.applyGlobals()
				}
			}
		}
	}()
	m.startTrackerLoop()
}

func isTerminal(status string) bool {
	return status == "complete" || status == "error" || status == "removed"
}

func (m *Manager) sync() {
	var tasks []db.DownloadTask
	if err := m.db.Where("status IN ?", []string{"active", "waiting", "paused"}).
		Find(&tasks).Error; err != nil {
		return
	}
	if len(tasks) == 0 {
		// 无活跃任务时低频探测 aria2 可达性,维护 degraded 标志
		if time.Now().Unix()%30 < 2 {
			_, err := m.c.GetVersion()
			m.degraded.Store(err != nil)
		}
		return
	}
	for i := range tasks {
		m.syncOne(&tasks[i])
	}
}

func (m *Manager) syncOne(t *db.DownloadTask) {
	st, err := m.c.TellStatus(t.GID)
	if err != nil {
		if strings.Contains(err.Error(), "is not found") {
			// aria2 重启丢了任务(未到终态):标记错误,历史保留。
			// 和 followedBy 迁移串行化：迁移可能已经把旧磁力记录换成
			// 真实任务，别用 Save(按主键 upsert)把旧记录又复活出来。
			m.migMu.Lock()
			var still db.DownloadTask
			if m.db.First(&still, "gid = ?", t.GID).Error == nil {
				t.Status = "error"
				t.ErrorMsg = "任务在 aria2 中丢失(aria2 可能重启过)"
				m.db.Save(t)
			}
			m.migMu.Unlock()
			return
		}
		m.degraded.Store(true)
		return
	}
	m.degraded.Store(false)

	// 磁力：aria2 取到元数据后会生成 paused 的真实下载，gid 在
	// followedBy 里；把数据库记录迁过去。
	if st.Status == "complete" && len(st.FollowedBy) > 0 {
		m.migMu.Lock()
		// 拿到锁后任务可能已被另一轮同步或用户操作迁移/删除。
		var still db.DownloadTask
		if err := m.db.First(&still, "gid = ?", t.GID).Error; err != nil {
			m.migMu.Unlock()
			return
		}
		newGID := st.FollowedBy[0]
		nt := db.DownloadTask{
			GID: newGID, URL: t.URL, Dir: t.Dir,
			Status: "paused", CreatedAt: t.CreatedAt,
			Follows: t.GID,
		}
		if err := m.replaceTaskRecord(t.GID, &nt); err != nil {
			m.migMu.Unlock()
			return
		}
		m.migMu.Unlock()
		m.syncOne(&nt)
		return
	}

	t.Status = st.Status
	t.TotalLength = parseI64(st.TotalLength)
	t.CompletedLength = parseI64(st.CompletedLength)
	t.ErrorMsg = friendlyErr(st.ErrorMessage)
	if name := statusName(st); name != "" {
		t.Name = name
	}
	m.db.Save(t)

	m.mu.Lock()
	if isTerminal(st.Status) {
		delete(m.speeds, t.GID)
	} else {
		m.speeds[t.GID] = parseI64(st.DownloadSpeed)
	}
	m.mu.Unlock()
}

// replaceTaskRecord 原子地把旧 gid 迁到新 gid。先保存新记录再删旧记录,
// 既保证 resolveGID 在迁移窗口中可用,也避免写库失败时把任务历史弄丢。
func (m *Manager) replaceTaskRecord(oldGID string, next *db.DownloadTask) error {
	return m.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(next).Error; err != nil {
			return err
		}
		return tx.Delete(&db.DownloadTask{}, "gid = ?", oldGID).Error
	})
}

func statusName(st *Status) string {
	if st.Bittorrent != nil && st.Bittorrent.Info != nil && st.Bittorrent.Info.Name != "" {
		return st.Bittorrent.Info.Name
	}
	if len(st.Files) > 0 && st.Files[0].Path != "" {
		base := path.Base(strings.ReplaceAll(st.Files[0].Path, "\\", "/"))
		if base != "" && base != "." && !strings.HasPrefix(base, "[METADATA]") {
			return base
		}
	}
	return ""
}

func parseI64(s string) int64 {
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

func validURL(raw string) error {
	if strings.HasPrefix(raw, "magnet:") {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return errors.New("链接格式不正确")
	}
	switch u.Scheme {
	case "http", "https", "ftp":
		return nil
	}
	return errors.New("仅支持 http/https/ftp 直链或磁力链接")
}

// taskOpts 组装任务级 aria2 选项:保存目录、做种时长,BT 类任务附加
// 最新 tracker 列表(对磁力与 .torrent 都生效)。
func (m *Manager) taskOpts(relDir string, bt bool) map[string]string {
	dir := m.dataRoot
	if relDir != "" {
		dir = path.Join(strings.ReplaceAll(m.dataRoot, "\\", "/"), relDir)
	}
	s := m.Settings()
	opts := map[string]string{
		"dir":       dir,
		"seed-time": strconv.Itoa(s.SeedTimeMin),
	}
	if bt {
		if t := m.effectiveTrackers(s); t != "" {
			opts["bt-tracker"] = t
		}
	}
	return opts
}

// aria2 是本机进程,只能写本机磁盘;外部存储(@挂载)不可作为下载目录
func validDownloadDir(relDir string) error {
	if strings.HasPrefix(relDir, "@") {
		return errors.New("离线下载只能保存到本机存储(下载完成后可再移动/上传到外部存储)")
	}
	return nil
}

// ensureDir 由 PocketDrive 自己把目标目录建出来,免得 aria2 去建。
// 只在 aria2 已经收下任务之后调:add 失败时(比如 aria2 没起来)不该
// 在用户网盘里留一个空文件夹。aria2 抢先建好了也无所谓,MkdirAll 是
// 幂等的。
func (m *Manager) ensureDir(relDir string) {
	if relDir == "" || m.localDir == "" {
		return
	}
	rel := cleanRel(relDir)
	if rel == "" || strings.HasPrefix(rel, "@") {
		return
	}
	_ = os.MkdirAll(filepath.Join(m.localDir, filepath.FromSlash(rel)), 0o755)
}

// friendlyErr 把 aria2 的原始报错翻成能照着做的中文。
//
// "Download aborted." 是 aria2 初始化文件失败时的外层文案，真正原因
// 往往只在 aria2 容器日志里。项目维护的 compose 已让两个容器共享同一个
// /data；如果用户自行修改了挂载路径或运行用户，这里给出可执行的排查提示。
func friendlyErr(msg string) string {
	const fix = "(请确认 PocketDrive 与 aria2 都挂载同一个 ./data:/data，" +
		"然后重建官方 compose；详细原因可看 docker compose logs aria2)"
	switch {
	case strings.Contains(msg, "Permission denied"):
		return msg + " " + fix
	case strings.HasPrefix(msg, "Download aborted"):
		return msg + " 多半是没有写入权限。" + fix
	}
	return msg
}

func (m *Manager) Add(rawURL, relDir string) (*db.DownloadTask, error) {
	if err := validURL(rawURL); err != nil {
		return nil, err
	}
	if err := validDownloadDir(relDir); err != nil {
		return nil, err
	}
	isMagnet := strings.HasPrefix(rawURL, "magnet:")
	opts := m.taskOpts(relDir, isMagnet)
	if isMagnet {
		if _, err := magnetHash(rawURL); err != nil {
			return nil, err
		}
		// 元数据任务本身必须运行；pause-metadata 只暂停 aria2 根据元数据
		// 创建的真实下载。这样先拿到完整文件清单，再由用户勾选并恢复，
		// 不会在弹框出现前偷跑数据。
		opts["pause-metadata"] = "true"
	}
	gid, err := m.c.AddURI(rawURL, opts)
	if err != nil {
		m.degraded.Store(true)
		return nil, errors.New("aria2 不可达或拒绝任务: " + err.Error())
	}
	m.degraded.Store(false)
	t := db.DownloadTask{GID: gid, URL: rawURL, Dir: relDir, Status: "active"}
	if err := m.db.Create(&t).Error; err != nil {
		_ = m.c.Remove(gid)
		_ = m.c.RemoveDownloadResult(gid)
		return nil, err
	}
	m.ensureDir(relDir)
	return &t, nil
}

// AddTorrent 用上传的 .torrent 文件(base64)创建 BT 任务。paused 为真时
// 任务先挂起(不下载任何数据),等前端展示文件清单、用户勾选后再下发
// select-file 并继续——「选完再下」的弹框流程靠这一步垫底。
func (m *Manager) AddTorrent(torrentB64, relDir, name string, paused bool) (*db.DownloadTask, error) {
	if err := validDownloadDir(relDir); err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(torrentB64)
	if err != nil {
		return nil, errors.New("种子文件内容不是合法的 base64")
	}
	// bencode 字典以 'd' 开头;顺手挡住误传的其他文件
	if len(raw) == 0 || raw[0] != 'd' {
		return nil, errors.New("不是有效的 .torrent 文件")
	}
	opts := m.taskOpts(relDir, true)
	if paused {
		opts["pause"] = "true"
	}
	gid, err := m.c.AddTorrent(torrentB64, opts)
	if err != nil {
		m.degraded.Store(true)
		return nil, errors.New("aria2 不可达或拒绝任务: " + err.Error())
	}
	m.degraded.Store(false)
	t := db.DownloadTask{
		GID: gid, URL: name, Dir: relDir, Status: "active",
		Name: strings.TrimSuffix(name, ".torrent"),
	}
	if paused {
		t.Status = "paused"
	}
	if err := m.db.Create(&t).Error; err != nil {
		_ = m.c.Remove(gid)
		_ = m.c.RemoveDownloadResult(gid)
		return nil, err
	}
	m.ensureDir(relDir)
	return &t, nil
}

// TorrentFile 是「选完再下」弹框里的一行:aria2 的 1-based 文件序号 +
// 完整路径 + 大小。
type TorrentFile struct {
	Index    int    `json:"index"`
	Path     string `json:"path"`
	Length   int64  `json:"length"`
	Selected bool   `json:"selected"`
}

// resolveGID 把前端传进来的 gid(磁力链拿到的通常是元数据 gid)解析成
// aria2 当前真实下载的 gid。磁力元数据就绪后 aria2 会 follow 出新 gid,
// 旧 gid 只留一条 [METADATA] 伪文件——拿旧 gid 永远等不出文件清单。
// 顺序:①库里迁移记录(follows=旧gid)→新 gid;②aria2 的 followedBy。
// 先查 follows 再查 gid：数据库迁移先建新记录后删旧记录，这个顺序
// 保证中间窗口里旧 gid 仍能翻到新 gid。
func (m *Manager) resolveGID(gid string) string {
	var t db.DownloadTask
	if err := m.db.Where("follows = ?", gid).First(&t).Error; err == nil {
		return t.GID
	}
	st, err := m.c.TellStatus(gid)
	if err == nil && len(st.FollowedBy) > 0 {
		return st.FollowedBy[0]
	}
	return gid
}

// TorrentFiles 返回 BT 任务的种子名与文件清单。磁力链在元数据下载完成
// 之前只有一条 [METADATA] 伪文件(前端据此判断「还不能选」);.torrent
// 添加后立刻就有完整清单。
func (m *Manager) TorrentFiles(gid string) (string, []TorrentFile, error) {
	cur := m.resolveGID(gid)
	st, err := m.c.TellStatus(cur)
	if err != nil {
		return "", nil, err
	}
	files := make([]TorrentFile, 0, len(st.Files))
	for i, f := range st.Files {
		files = append(files, TorrentFile{
			Index:  i + 1,
			Path:   f.Path,
			Length: parseI64(f.Length),
			// aria2 对旧版本或测试桩未返回 selected 时按默认选中处理。
			Selected: f.Selected != "false",
		})
	}
	return statusName(st), files, nil
}

// SelectFiles 应用用户勾选的文件序号后开始下载。files 是 aria2 的
// 1-based 序号;不传则下载全部。调用时任务应在 paused 状态——种子添加
// 时直接挂起，磁力生成的真实任务由 pause-metadata 自动挂起。
func (m *Manager) SelectFiles(gid string, files []int) error {
	cur := m.resolveGID(gid)
	if len(files) > 0 {
		parts := make([]string, len(files))
		for i, f := range files {
			parts[i] = strconv.Itoa(f)
		}
		if err := m.c.ChangeOption(cur, map[string]string{"select-file": strings.Join(parts, ",")}); err != nil {
			return err
		}
	}
	if err := m.c.Unpause(cur); err != nil {
		return err
	}
	m.db.Model(&db.DownloadTask{}).Where("gid = ? OR follows = ?", cur, gid).Update("status", "active")
	return nil
}

type taskView struct {
	db.DownloadTask
	Speed int64 `json:"speed"`
}

func (m *Manager) List() (bool, []taskView) {
	var tasks []db.DownloadTask
	m.db.Order("created_at DESC").Limit(100).Find(&tasks)
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]taskView, len(tasks))
	for i, t := range tasks {
		out[i] = taskView{DownloadTask: t, Speed: m.speeds[t.GID]}
	}
	return m.degraded.Load(), out
}

// ---- HTTP handlers ----

func (m *Manager) HandleList(w http.ResponseWriter, r *http.Request) {
	degraded, tasks := m.List()
	httpx.JSON(w, http.StatusOK, map[string]any{"degraded": degraded, "tasks": tasks})
}

func (m *Manager) HandleAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
		Dir string `json:"dir"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	relDir := cleanRel(req.Dir)
	t, err := m.Add(strings.TrimSpace(req.URL), relDir)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "task": t})
}

func cleanRel(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = path.Clean("/" + p)
	return strings.TrimPrefix(p, "/")
}

// HandleAddTorrent 接收 {torrent: base64, name, dir, paused};.torrent 文件
// 通常几十 KB,超多文件的种子也就几 MB,放宽到 16MB 上限。paused 用于
// 「先展示文件清单让用户勾选,再正式开始下载」的流程。
func (m *Manager) HandleAddTorrent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Torrent string `json:"torrent"`
		Name    string `json:"name"`
		Dir     string `json:"dir"`
		Paused  bool   `json:"paused"`
	}
	if err := httpx.DecodeN(r, &req, 16<<20); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误(种子文件过大?)")
		return
	}
	if req.Torrent == "" {
		httpx.Err(w, http.StatusBadRequest, "缺少种子文件内容")
		return
	}
	name := path.Base(strings.ReplaceAll(strings.TrimSpace(req.Name), "\\", "/"))
	if name == "" || name == "." {
		name = "upload.torrent"
	}
	t, err := m.AddTorrent(req.Torrent, cleanRel(req.Dir), name, req.Paused)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "task": t})
}

// HandleTorrentFiles 返回某个 BT 任务(种子或磁力)的文件清单,供前端
// 弹框里打勾。gid 走 URL 路径参数,匹配 GET /api/v1/downloads/{gid}/files。
func (m *Manager) HandleTorrentFiles(w http.ResponseWriter, r *http.Request) {
	gid := r.PathValue("gid")
	if gid == "" {
		httpx.Err(w, http.StatusBadRequest, "缺少 gid")
		return
	}
	name, files, err := m.TorrentFiles(gid)
	if err != nil {
		httpx.Err(w, http.StatusBadGateway, "获取文件列表失败: "+err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"name": name, "files": files})
}

// HandleSelectFiles 确认弹框里勾选的文件,把 select-file 下发给 aria2
// 并恢复下载。gid 走 URL 路径参数,匹配 POST /api/v1/downloads/{gid}/select。
func (m *Manager) HandleSelectFiles(w http.ResponseWriter, r *http.Request) {
	gid := r.PathValue("gid")
	if gid == "" {
		httpx.Err(w, http.StatusBadRequest, "缺少 gid")
		return
	}
	var req struct {
		Files []int `json:"files"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := m.SelectFiles(gid, req.Files); err != nil {
		httpx.Err(w, http.StatusBadGateway, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (m *Manager) gidReq(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req struct {
		GID string `json:"gid"`
	}
	if err := httpx.Decode(r, &req); err != nil || req.GID == "" {
		httpx.Err(w, http.StatusBadRequest, "缺少 gid")
		return "", false
	}
	return req.GID, true
}

func (m *Manager) HandlePause(w http.ResponseWriter, r *http.Request) {
	gid, ok := m.gidReq(w, r)
	if !ok {
		return
	}
	cur := m.resolveGID(gid)
	if err := m.c.Pause(cur); err != nil {
		httpx.Err(w, http.StatusBadGateway, err.Error())
		return
	}
	m.db.Model(&db.DownloadTask{}).Where("gid = ? OR follows = ?", cur, gid).Update("status", "paused")
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (m *Manager) HandleUnpause(w http.ResponseWriter, r *http.Request) {
	gid, ok := m.gidReq(w, r)
	if !ok {
		return
	}
	cur := m.resolveGID(gid)
	if err := m.c.Unpause(cur); err != nil {
		httpx.Err(w, http.StatusBadGateway, err.Error())
		return
	}
	m.db.Model(&db.DownloadTask{}).Where("gid = ? OR follows = ?", cur, gid).Update("status", "active")
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// HandleRemove 删除任务;deleteFiles 为真时连已下载的文件一并删除。
func (m *Manager) HandleRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		GID         string `json:"gid"`
		DeleteFiles bool   `json:"deleteFiles"`
	}
	if err := httpx.Decode(r, &req); err != nil || req.GID == "" {
		httpx.Err(w, http.StatusBadRequest, "缺少 gid")
		return
	}
	deleted, err := m.RemoveTask(req.GID, req.DeleteFiles)
	if err != nil {
		httpx.Err(w, http.StatusNotFound, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "deletedFiles": deleted})
}
