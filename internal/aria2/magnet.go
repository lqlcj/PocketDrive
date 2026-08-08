package aria2

// 磁力链接 → .torrent 种子:解决 aria2 容器拉不到元数据(磁力没人做种、
// DHT/B、BT 端口被防火墙挡)时,种子文件上传却一直好用的落差。
//
// 思路:添加磁力时把磁力交给 aria2 但**挂起**(不挂起的话 aria2 自己
// 拉到元数据后会立刻 follow 成真实下载、未经勾选就写盘),元数据由
// PocketDrive 自己用 anacrolix/torrent 走 DHT + 注入的全套 tracker 去取。
// 取到之后转成 .torrent,走 AddTorrent 这条「上传种子」的成熟路,并把
// 库里记录迁到新 gid(follows=旧 gid,前端拿旧 gid 轮询也能解析到)。
// 转换失败就保持挂起,前端会提示换 .torrent 上传;不会偷下载任何数据。
//
// 同步的还有一个表亲:.torrent 链接(http(s) 直链)直接交给 aria2 只会
// 被当成普通文件下载、永远解析不出元数据。由 torrentlink.go 把链接下载
// 成种子文件,再走同一条 AddTorrent 熟路——转换骨架(startConvert)在这里
// 共用,去重与失败退避两者通用。

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anacrolix/torrent"

	"pocketdrive/internal/db"
)

// magnetConvertTimeout 给元数据取数的时间上限。前端弹框的磁力超时是
// 120 秒,这里必须留在这个值之内,转换成功才能赶在弹框转「失败」之前
// 把文件清单交出来。
const magnetConvertTimeout = 110 * time.Second

// magnetRetryGap 一次转换失败后至少隔这么久才允许再次尝试,免得前端
// 每秒轮询一次,失败就一直轰 DHT。
const magnetRetryGap = 5 * time.Second

// magnetJob 记录一个磁力链接的转换状态,按 gid 存在 Manager.magnetJobs 里。
// running 防重入(同一磁力只转一次);lastErrAt 用于失败后的退避重试。
type magnetJob struct {
	gid       string
	running   bool
	lastErrAt time.Time
}

// magnetHash 从磁力链接里抠出 40 位 hex 的 btih 信息哈希。
func magnetHash(magnet string) (string, error) {
	idx := strings.Index(strings.ToLower(magnet), "urn:btih:")
	if idx < 0 {
		return "", errors.New("磁力链接缺少 btih 信息哈希")
	}
	rest := magnet[idx+len("urn:btih:"):]
	hash := ""
	for _, c := range rest {
		if c == '&' {
			break
		}
		hash += string(c)
	}
	hash = strings.TrimSpace(hash)
	if len(hash) != 40 {
		return "", errors.New("磁力链接的 btih 信息哈希不是 40 位")
	}
	for _, c := range strings.ToLower(hash) {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return "", errors.New("磁力链接的 btih 信息哈希不是 hex 编码")
		}
	}
	return strings.ToLower(hash), nil
}

// withTrackers 把库里维护的最新 tracker 列表追加进磁力链接(tr 参数),
// anacrolix 取元数据时就会挨个去 announce,比 aria2 那一条条碰运气稳得多。
func withTrackers(magnet, trackers string) string {
	for _, tr := range strings.Split(trackers, ",") {
		tr = strings.TrimSpace(tr)
		if tr == "" {
			continue
		}
		magnet += "&tr=" + url.QueryEscape(tr)
	}
	return magnet
}

// startMagnetConvert 后台取磁力元数据 → 转 .torrent → 走 AddTorrent。
// 由 startConvert 兜底去重与失败退避,这里只负责「怎么拿到种子」。
func (m *Manager) startMagnetConvert(oldGID, magnet, relDir string) {
	m.startConvert(oldGID, magnet, relDir, func() (string, string, error) {
		return fetchTorrentMeta(withTrackers(magnet, m.trackers()))
	})
}

// startConvert 是「磁力转种子」与「种子链接转种子」共用的转换骨架。
// 同一 gid 只允许一个转换在跑;失败后记下 lastErrAt,前端「重试」走到
// ensureConvert 时按 magnetRetryGap 退避后再拉起。取种子成功就交给
// finishMagnet 收尾(AddTorrent 挂起 + 库记录迁移,follows=旧 gid)。
func (m *Manager) startConvert(oldGID, seedURL, relDir string, grab func() (string, string, error)) {
	m.magnetMu.Lock()
	j := m.magnetJobs[oldGID]
	if j == nil {
		j = &magnetJob{gid: oldGID}
		m.magnetJobs[oldGID] = j
	}
	if j.running {
		m.magnetMu.Unlock()
		return // 已有转换在跑,别重复
	}
	if !j.lastErrAt.IsZero() && time.Since(j.lastErrAt) < magnetRetryGap {
		m.magnetMu.Unlock()
		return // 刚失败过,退避一下
	}
	j.running = true
	m.magnetMu.Unlock()

	go func() {
		torrentB64, name, err := grab()
		m.magnetMu.Lock()
		if err != nil {
			j.running = false
			j.lastErrAt = time.Now()
			m.magnetMu.Unlock()
			return
		}
		// 成功:跑完 finishMagnet(迁移记录)后再摘掉 job
		m.magnetMu.Unlock()
		ok := m.finishMagnet(oldGID, seedURL, relDir, torrentB64, name)
		m.magnetMu.Lock()
		if ok {
			delete(m.magnetJobs, oldGID)
		} else {
			// AddTorrent 被 aria2 拒了:标记可重试,别让 job 卡在 running
			j.running = false
			j.lastErrAt = time.Now()
		}
		m.magnetMu.Unlock()
	}()
}

// ensureConvert 保证某个待转换任务(磁力链接 / .torrent 链接)有转换在跑。
// 首次由 Add 拉起;转换失败后,前端「重试」重新轮询文件清单时会走到
// 这里,把转换重新拉起。
func (m *Manager) ensureConvert(gid string) {
	var t db.DownloadTask
	if err := m.db.First(&t, "gid = ?", gid).Error; err != nil {
		return
	}
	// 已经迁成种子任务(follows 非空)、或非磁力/种子链接、或已是终态:不用转
	isMagnet := strings.HasPrefix(t.URL, "magnet:")
	if t.Follows != "" || (!isMagnet && !isTorrentLink(t.URL)) || isTerminal(t.Status) {
		return
	}
	if isMagnet {
		m.startMagnetConvert(gid, t.URL, t.Dir)
	} else {
		m.startTorrentLinkConvert(gid, t.URL, t.Dir)
	}
}

// fetchTorrentMeta 用 anacrolix 的 BT 客户端取磁力元数据,返回可直接喂给
// aria2.addTorrent 的 base64 .torrent 内容,以及种子名。
func fetchTorrentMeta(magnet string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), magnetConvertTimeout)
	defer cancel()

	cfg := torrent.NewDefaultClientConfig()
	cfg.NoUpload = true
	cfg.ListenPort = 0 // 随机端口,DHT 走的是外连,UDP 入向不通也能用
	cfg.DataDir = filepath.Join(os.TempDir(), "pocketdrive-magnet")
	cfg.Slogger = slog.New(slog.NewTextHandler(io.Discard, nil))

	cl, err := torrent.NewClient(cfg)
	if err != nil {
		return "", "", errors.New("初始化 BT 客户端失败: " + err.Error())
	}
	defer cl.Close()

	t, err := cl.AddMagnet(magnet)
	if err != nil {
		return "", "", errors.New("解析磁力链接失败: " + err.Error())
	}
	defer t.Drop()

	select {
	case <-t.GotInfo():
	case <-ctx.Done():
		return "", "", errors.New("110 秒没取到元数据:磁力无人做种,或 DHT/tracker 全都不通")
	}

	mi := t.Metainfo()
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return "", "", errors.New("解析取到的种子信息失败: " + err.Error())
	}

	var buf strings.Builder
	if err := mi.Write(&buf); err != nil {
		return "", "", errors.New("序列化种子失败: " + err.Error())
	}

	b64 := base64.StdEncoding.EncodeToString([]byte(buf.String()))
	return b64, info.Name, nil
}

// finishMagnet 在种子内容(磁力元数据到手 / .torrent 链接下载完成)之后,
// 把 aria2 里的挂起任务换成 .torrent 任务,并迁移库里记录。调用方
// (转换 goroutine)与 syncOne 可能并发迁移同一任务,用 migMu 串行化,
// 进去后先确认旧记录还在。
// 返回 false 表示 .torrent 没加进 aria2(调用方应让任务可重试)。
func (m *Manager) finishMagnet(oldGID, seedURL, relDir, torrentB64, name string) bool {
	m.migMu.Lock()
	defer m.migMu.Unlock()

	var t db.DownloadTask
	if err := m.db.First(&t, "gid = ?", oldGID).Error; err != nil {
		return true // 任务已被删除,或 aria2 已抢先 follow 迁移
	}
	if isTerminal(t.Status) {
		return true
	}

	opts := m.taskOpts(relDir, true)
	opts["pause"] = "true" // 先挂起,等前端弹框勾选文件后再开始
	newGID, err := m.c.AddTorrent(torrentB64, opts)
	if err != nil {
		return false // 留着原挂起任务,等下次重试
	}
	// 让 aria2 忘掉旧的挂起任务,免得它 follow 完再迁出一条重复任务
	_ = m.c.Remove(oldGID)
	_ = m.c.RemoveDownloadResult(oldGID)
	m.ensureDir(relDir)

	// 先建新记录再删旧的:resolveGID 优先按 follows 翻,中间窗口里旧
	// gid 也一直能解析到新 gid,前端轮询不会断档
	nt := db.DownloadTask{
		GID: newGID, URL: seedURL, Dir: t.Dir,
		Status: "paused", Name: name,
		Follows: oldGID, CreatedAt: t.CreatedAt,
	}
	m.db.Create(&nt)
	m.db.Delete(&db.DownloadTask{}, "gid = ?", oldGID)
	return true
}
