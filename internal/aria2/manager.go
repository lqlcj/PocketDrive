package aria2

import (
	"errors"
	"net/http"
	"net/url"
	"path"
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
	degraded atomic.Bool

	mu     sync.Mutex
	speeds map[string]int64

	stop chan struct{}
}

func NewManager(gdb *gorm.DB, c *Client, dataRoot string) *Manager {
	return &Manager{
		db:       gdb,
		c:        c,
		dataRoot: dataRoot,
		speeds:   make(map[string]int64),
		stop:     make(chan struct{}),
	}
}

func (m *Manager) Start() {
	go func() {
		// 启动即探测一次,让前端在无任务时也能显示 aria2 连接状态
		_, err := m.c.GetVersion()
		m.degraded.Store(err != nil)

		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-m.stop:
				return
			case <-t.C:
				m.sync()
			}
		}
	}()
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
			// aria2 重启丢了任务(未到终态):标记错误,历史保留
			t.Status = "error"
			t.ErrorMsg = "任务在 aria2 中丢失(aria2 可能重启过)"
			m.db.Save(t)
			return
		}
		m.degraded.Store(true)
		return
	}
	m.degraded.Store(false)

	// 磁力:元数据下载完成后真正的下载在 followedBy gid 里,迁移记录
	if st.Status == "complete" && len(st.FollowedBy) > 0 {
		newGID := st.FollowedBy[0]
		m.db.Delete(&db.DownloadTask{}, "gid = ?", t.GID)
		nt := db.DownloadTask{
			GID: newGID, URL: t.URL, Dir: t.Dir,
			Status: "active", CreatedAt: t.CreatedAt,
		}
		m.db.Save(&nt)
		m.syncOne(&nt)
		return
	}

	t.Status = st.Status
	t.TotalLength = parseI64(st.TotalLength)
	t.CompletedLength = parseI64(st.CompletedLength)
	t.ErrorMsg = st.ErrorMessage
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

func (m *Manager) Add(rawURL, relDir string) (*db.DownloadTask, error) {
	if err := validURL(rawURL); err != nil {
		return nil, err
	}
	dir := m.dataRoot
	if relDir != "" {
		dir = path.Join(strings.ReplaceAll(m.dataRoot, "\\", "/"), relDir)
	}
	gid, err := m.c.AddURI(rawURL, dir)
	if err != nil {
		m.degraded.Store(true)
		return nil, errors.New("aria2 不可达或拒绝任务: " + err.Error())
	}
	m.degraded.Store(false)
	t := db.DownloadTask{GID: gid, URL: rawURL, Dir: relDir, Status: "active"}
	if err := m.db.Create(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
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
	if err := m.c.Pause(gid); err != nil {
		httpx.Err(w, http.StatusBadGateway, err.Error())
		return
	}
	m.db.Model(&db.DownloadTask{}).Where("gid = ?", gid).Update("status", "paused")
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (m *Manager) HandleUnpause(w http.ResponseWriter, r *http.Request) {
	gid, ok := m.gidReq(w, r)
	if !ok {
		return
	}
	if err := m.c.Unpause(gid); err != nil {
		httpx.Err(w, http.StatusBadGateway, err.Error())
		return
	}
	m.db.Model(&db.DownloadTask{}).Where("gid = ?", gid).Update("status", "active")
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (m *Manager) HandleRemove(w http.ResponseWriter, r *http.Request) {
	gid, ok := m.gidReq(w, r)
	if !ok {
		return
	}
	var t db.DownloadTask
	if err := m.db.First(&t, "gid = ?", gid).Error; err != nil {
		httpx.Err(w, http.StatusNotFound, "任务不存在")
		return
	}
	if !isTerminal(t.Status) {
		_ = m.c.Remove(gid) // 忽略错误:aria2 里可能已不存在
	}
	_ = m.c.RemoveDownloadResult(gid)
	m.db.Delete(&db.DownloadTask{}, "gid = ?", gid)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}
