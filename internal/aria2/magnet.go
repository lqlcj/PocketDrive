package aria2

// 磁力链接 → .torrent 种子:解决 aria2 容器拉不到元数据(磁力没人做种、
// DHT/BT 端口被防火墙挡)时,种子文件上传却一直好用的落差。
//
// 思路:添加磁力时照旧把磁力交给 aria2 挂着拉元数据(这样前端拿到的
// gid 是真实的,轮询 [METADATA] 伪文件的逻辑不用动),同时 PocketDrive
// 自己用 anacrolix/torrent 走 DHT + 注入的全套 tracker 去取元数据。
// 取到之后转成 .torrent,走 AddTorrent 这条「上传种子」的熟路,并把
// 库里记录迁到新 gid(follows=旧gid,前端拿旧 gid 轮询也能解析到)。
// 转换失败就什么都不做,原磁力任务还留在 aria2 里,行为同改造前。

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
	"pocketdrive/internal/logs"
)

// magnetConvertTimeout 给元数据取数的时间上限。前端弹框的磁力超时是
// 120 秒,这里必须留在这个值之内,转换成功才能赶在弹框转「失败」之前
// 把文件清单交出来。
const magnetConvertTimeout = 110 * time.Second

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
// 转换是尽力而为:失败只记日志,不干扰 aria2 里挂着的原磁力任务。
func (m *Manager) startMagnetConvert(oldGID, magnet, relDir, hash string) {
	go func() {
		torrentB64, name, err := fetchTorrentMeta(withTrackers(magnet, m.trackers()))
		if err != nil {
			logs.Errorf("aria2", "磁力转种子失败(%s…): %v", hash[:8], err)
			return
		}
		m.finishMagnet(oldGID, magnet, relDir, torrentB64, name)
	}()
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

// finishMagnet 在磁力元数据取到之后,把 aria2 里的磁力任务换成
// .torrent 任务,并迁移库里记录。调用方(转换 goroutine)与 syncOne
// 可能并发迁移同一任务,用 migMu 串行化,进去后先确认旧记录还在。
func (m *Manager) finishMagnet(oldGID, magnet, relDir, torrentB64, name string) {
	m.migMu.Lock()
	defer m.migMu.Unlock()

	var t db.DownloadTask
	if err := m.db.First(&t, "gid = ?", oldGID).Error; err != nil {
		return // 任务已被删除,或 aria2 已抢先 follow 迁移
	}
	if isTerminal(t.Status) {
		return
	}

	opts := m.taskOpts(relDir, true)
	opts["pause"] = "true" // 先挂起,等前端弹框勾选文件后再开始
	newGID, err := m.c.AddTorrent(torrentB64, opts)
	if err != nil {
		logs.Errorf("aria2", "磁力转种子后添加任务失败(%s): %v", oldGID, err)
		return // 留着原磁力任务,让 aria2 继续等
	}
	// 让 aria2 忘掉磁力元数据任务,免得它 follow 完再迁出一条重复任务
	_ = m.c.Remove(oldGID)
	_ = m.c.RemoveDownloadResult(oldGID)
	m.ensureDir(relDir)

	// 先建新记录再删旧的:resolveGID 优先按 follows 翻,中间窗口里旧
	// gid 也一直能解析到新 gid,前端轮询不会断档
	nt := db.DownloadTask{
		GID: newGID, URL: magnet, Dir: t.Dir,
		Status: "paused", Name: name,
		Follows: oldGID, CreatedAt: t.CreatedAt,
	}
	m.db.Create(&nt)
	m.db.Delete(&db.DownloadTask{}, "gid = ?", oldGID)
	logs.Errorf("aria2", "磁力转种子成功:%s → %s", name, newGID)
}
