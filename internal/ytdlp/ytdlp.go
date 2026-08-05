// Package ytdlp runs yt-dlp downloads as a single-worker job queue.
// Security model: exec.Command direct invocation (no shell), URLs
// restricted to http/https, arguments come from fixed preset templates
// only — user input is never spliced into flags.
package ytdlp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"pocketdrive/internal/db"
	"pocketdrive/internal/httpx"
	"pocketdrive/internal/logs"
)

const logTailLines = 40

var (
	rePercent = regexp.MustCompile(`\[download\]\s+([0-9.]+)%`)
	reDest    = regexp.MustCompile(`(?:Destination:|Merging formats into ")\s*(.+?)"?\s*$`)
	// 播放列表模式:[download] Downloading item 3 of 25
	reItem = regexp.MustCompile(`\[download\] Downloading item (\d+) of (\d+)`)
)

type Manager struct {
	db      *gorm.DB
	bin     string
	dataDir string
	// confDir 存 cookies 之类的私密配置(DB 同级,不在网盘里)
	confDir string
	queue   chan uint

	mu      sync.Mutex
	cancels map[uint]context.CancelFunc

	verMu      sync.Mutex
	version    string
	verChecked time.Time
}

func NewManager(gdb *gorm.DB, bin, dataDir, confDir string) *Manager {
	m := &Manager{
		db:      gdb,
		bin:     bin,
		dataDir: dataDir,
		confDir: confDir,
		queue:   make(chan uint, 64),
		cancels: make(map[uint]context.CancelFunc),
	}
	// 服务重启时,上一轮遗留的 queued/running 任务标记为中断
	gdb.Model(&db.YtdlpTask{}).
		Where("status IN ?", []string{"queued", "running"}).
		Updates(map[string]any{"status": "error", "error_msg": "服务重启,任务中断"})
	return m
}

func (m *Manager) Start() {
	go func() {
		for id := range m.queue {
			m.run(id)
		}
	}()
}

// Available reports whether the yt-dlp binary exists, plus its version.
func (m *Manager) Available() (bool, string) {
	m.verMu.Lock()
	defer m.verMu.Unlock()
	if time.Since(m.verChecked) < 10*time.Minute {
		return m.version != "", m.version
	}
	m.verChecked = time.Now()
	if _, err := exec.LookPath(m.bin); err != nil {
		m.version = ""
		return false, ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, m.bin, "--version").Output()
	if err != nil {
		m.version = ""
		return false, ""
	}
	m.version = strings.TrimSpace(string(out))
	return true, m.version
}

func (m *Manager) Add(rawURL, relDir, preset string, opts Options) (*db.YtdlpTask, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("仅支持 http/https 视频页链接")
	}
	if strings.HasPrefix(relDir, "@") {
		return nil, errors.New("yt下载只能保存到本机存储(下载完成后可再移动/上传到外部存储)")
	}
	np, ok := normalizePreset(preset)
	if !ok {
		return nil, errors.New("未知格式预设")
	}
	if ok, _ := m.Available(); !ok {
		return nil, errors.New("yt-dlp 不可用(未安装或不在 PATH)")
	}
	optJSON, _ := json.Marshal(opts)
	t := db.YtdlpTask{URL: u.String(), Dir: relDir, Preset: np, Options: string(optJSON), Status: "queued"}
	if err := m.db.Create(&t).Error; err != nil {
		return nil, err
	}
	select {
	case m.queue <- t.ID:
	default:
		m.db.Delete(&t)
		return nil, errors.New("队列已满,请稍后再试")
	}
	return &t, nil
}

// Options are user-selectable toggles, each mapping to fixed flags.
type Options struct {
	EmbedMeta  bool `json:"embedMeta"`
	EmbedThumb bool `json:"embedThumb"`
	Subs       bool `json:"subs"`
	// Playlist 为 true 时整个播放列表批量下载(默认只下当前单集)
	Playlist bool `json:"playlist"`
}

// presets 是服务端固定白名单:用户只能选 key,参数模板不可注入。
// 每个格式串都带多级回退:先要 mp4/m4a 的分离流(画质最好),
// 拿不到就退回任意容器分离流(bv*+ba,靠 --merge-output-format 收成 mp4),
// 再退到合成格式(b)。tv 等受限客户端返回的格式表较小,没这层兜底会
// 报 "Requested format is not available"。
var presets = map[string][]string{
	"video_best": {"-f", "bv*[ext=mp4]+ba[ext=m4a]/bv*+ba/b[ext=mp4]/b", "--merge-output-format", "mp4"},
	"video_1080": {"-f", "bv*[ext=mp4][height<=1080]+ba[ext=m4a]/bv*[height<=1080]+ba/b[ext=mp4][height<=1080]/b[height<=1080]/b", "--merge-output-format", "mp4"},
	"video_720":  {"-f", "bv*[ext=mp4][height<=720]+ba[ext=m4a]/bv*[height<=720]+ba/b[ext=mp4][height<=720]/b[height<=720]/b", "--merge-output-format", "mp4"},
	"video_480":  {"-f", "bv*[ext=mp4][height<=480]+ba[ext=m4a]/bv*[height<=480]+ba/b[ext=mp4][height<=480]/b[height<=480]/b", "--merge-output-format", "mp4"},
	// 音频:多数站点音轨本就是 m4a,仅 remux;mp3 需转码但音频转码很快。
	// 末尾的 /best 兜底:tv 等受限客户端拿不到独立音轨时,退回最佳合成格式
	// 再用 -x 抽音轨,避免 "Requested format is not available"。
	"audio_m4a": {"-f", "bestaudio/best", "-x", "--audio-format", "m4a"},
	"audio_mp3": {"-f", "bestaudio/best", "-x", "--audio-format", "mp3", "--audio-quality", "0"},
}

// 旧版任务记录里的预设名兼容
var legacyPresets = map[string]string{"video": "video_best", "audio": "audio_m4a"}

func normalizePreset(p string) (string, bool) {
	if np, ok := legacyPresets[p]; ok {
		return np, true
	}
	_, ok := presets[p]
	return p, ok
}

func presetArgs(preset string, opts Options) []string {
	name, ok := normalizePreset(preset)
	if !ok {
		name = "video_best"
	}
	args := append([]string{}, presets[name]...)
	if opts.EmbedMeta {
		args = append(args, "--embed-metadata")
	}
	if opts.EmbedThumb {
		args = append(args, "--embed-thumbnail")
	}
	if opts.Subs {
		args = append(args, "--write-subs", "--sub-langs", "zh-Hans,zh,en", "--convert-subs", "srt")
	}
	return args
}

// networkArgs 组装 cookies / 代理 / player client 三个设置项。
func (m *Manager) networkArgs() []string {
	s := m.Settings()
	var args []string
	if s.Proxy != "" {
		args = append(args, "--proxy", s.Proxy)
	}
	if c := m.runCookies(); c != "" {
		args = append(args, "--cookies", c)
	}
	if s.PlayerClient != "" && playerClients[s.PlayerClient] {
		args = append(args, "--extractor-args", "youtube:player_client="+s.PlayerClient)
	}
	return args
}

// hintFor 在 yt-dlp 的报错后面补一句能照着做的话。yt-dlp 原文是英文
// 且指向 wiki,对着网页用的人看不懂也不方便去翻。
func hintFor(log string) string {
	switch {
	case strings.Contains(log, "not a bot") || strings.Contains(log, "Sign in to confirm"):
		return "\n\n[PocketDrive] YouTube 把这台服务器的 IP 当成了机器人。" +
			"到本页「高级设置」里传一份浏览器导出的 cookies.txt(或配个代理)再试。"
	case strings.Contains(log, "Private video") || strings.Contains(log, "members-only"):
		return "\n\n[PocketDrive] 这是私有/会员视频,需要在「高级设置」里配置对应账号的 cookies。"
	case strings.Contains(log, "age") && strings.Contains(log, "confirm"):
		return "\n\n[PocketDrive] 年龄限制视频,需要在「高级设置」里配置已登录账号的 cookies。"
	case strings.Contains(log, "ffmpeg") && strings.Contains(log, "not installed"):
		return "\n\n[PocketDrive] 缺 ffmpeg,音视频合并做不了。"
	case strings.Contains(log, "Requested format is not available"):
		return "\n\n[PocketDrive] 当前播放器客户端返回的格式里没有你要的那种。" +
			"到「高级设置」把播放器客户端从 tv 改成默认或 web_safari 再试;音频任务会自动退而求其次找可用的格式。"
	}
	return ""
}

func (m *Manager) run(id uint) {
	var t db.YtdlpTask
	if err := m.db.First(&t, id).Error; err != nil {
		return
	}
	if t.Status != "queued" { // 已被取消
		return
	}

	absDir := filepath.Join(m.dataDir, filepath.FromSlash(t.Dir))
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		m.finish(&t, "error", "创建目标文件夹失败: "+err.Error())
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancels[id] = cancel
	m.mu.Unlock()
	defer func() {
		cancel()
		m.mu.Lock()
		delete(m.cancels, id)
		m.mu.Unlock()
	}()

	var opts Options
	_ = json.Unmarshal([]byte(t.Options), &opts)
	args := []string{"--newline", "--no-warnings"}
	// 反机器人相关的三样(cookies / 代理 / player client)全部来自设置,
	// 值走白名单或格式校验,不存在把用户输入拼成别的参数的可能
	args = append(args, m.networkArgs()...)
	if opts.Playlist {
		// 整个播放列表:存进「播放列表名」子文件夹,文件名带序号;
		// 模板变量由 yt-dlp 按操作系统清洗,不会产生路径穿越
		args = append(args, "--yes-playlist",
			"-o", filepath.Join(absDir, "%(playlist_title,playlist_id|合集)s", "%(playlist_index|0)s - %(title)s.%(ext)s"))
	} else {
		args = append(args, "--no-playlist",
			"-o", filepath.Join(absDir, "%(title)s.%(ext)s"))
	}
	args = append(args, presetArgs(t.Preset, opts)...)
	args = append(args, t.URL)

	cmd := exec.CommandContext(ctx, m.bin, args...)
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	t.Status = "running"
	m.db.Save(&t)

	if err := cmd.Start(); err != nil {
		m.finish(&t, "error", "启动 yt-dlp 失败: "+err.Error())
		return
	}

	done := make(chan struct{})
	go func() {
		m.pump(&t, pr)
		close(done)
	}()

	err := cmd.Wait()
	pw.Close()
	<-done

	switch {
	case ctx.Err() != nil:
		m.finish(&t, "canceled", "")
	case err != nil:
		m.finish(&t, "error", lastLines(t.LogTail, 5)+hintFor(t.LogTail))
	default:
		t.Progress = 100
		m.finish(&t, "done", "")
	}
}

// pump reads merged stdout/stderr, tracking progress and a log tail.
// 播放列表任务的总进度 = (已完成条目数*100 + 当前条目百分比) / 总条目数。
func (m *Manager) pump(t *db.YtdlpTask, r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 256*1024)
	var lines []string
	plItem, plTotal := 0, 0
	lastSave := time.Now()
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" {
			continue
		}
		lines = append(lines, line)
		if len(lines) > logTailLines {
			lines = lines[len(lines)-logTailLines:]
		}
		if mm := reItem.FindStringSubmatch(line); mm != nil {
			plItem, _ = strconv.Atoi(mm[1])
			plTotal, _ = strconv.Atoi(mm[2])
		}
		if mm := rePercent.FindStringSubmatch(line); mm != nil {
			if v, err := strconv.ParseFloat(mm[1], 64); err == nil {
				if plTotal > 1 {
					t.Progress = (float64(plItem-1)*100 + v) / float64(plTotal)
				} else {
					t.Progress = v
				}
			}
		}
		if mm := reDest.FindStringSubmatch(line); mm != nil {
			base := path.Base(strings.ReplaceAll(mm[1], "\\", "/"))
			if plTotal > 1 {
				t.Title = "[" + strconv.Itoa(plItem) + "/" + strconv.Itoa(plTotal) + "] " + base
			} else {
				t.Title = base
			}
		}
		if time.Since(lastSave) > 700*time.Millisecond {
			t.LogTail = strings.Join(lines, "\n")
			m.db.Save(t)
			lastSave = time.Now()
		}
	}
	t.LogTail = strings.Join(lines, "\n")
	m.db.Save(t)
}

func (m *Manager) finish(t *db.YtdlpTask, status, errMsg string) {
	t.Status = status
	t.ErrorMsg = errMsg
	m.db.Save(t)
	// 后台任务没有请求可以挂错误,失败只写在任务卡上,页面一关就没了
	if status == "error" {
		logs.Errorf("ytdlp", "任务 #%d 失败 (%s): %s", t.ID, t.URL, errMsg)
	}
}

func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func (m *Manager) Cancel(id uint) error {
	m.mu.Lock()
	cancel, running := m.cancels[id]
	m.mu.Unlock()
	if running {
		cancel()
		return nil
	}
	return m.db.Model(&db.YtdlpTask{}).
		Where("id = ? AND status = ?", id, "queued").
		Update("status", "canceled").Error
}

// ---- HTTP handlers ----

func (m *Manager) HandleList(w http.ResponseWriter, r *http.Request) {
	var tasks []db.YtdlpTask
	m.db.Order("created_at DESC").Limit(100).Find(&tasks)
	ok, ver := m.Available()
	httpx.JSON(w, http.StatusOK, map[string]any{
		"available": ok, "version": ver, "tasks": tasks,
	})
}

func (m *Manager) HandleAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL     string  `json:"url"`
		Dir     string  `json:"dir"`
		Preset  string  `json:"preset"`
		Options Options `json:"options"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	relDir := strings.TrimPrefix(path.Clean("/"+strings.ReplaceAll(req.Dir, "\\", "/")), "/")
	t, err := m.Add(req.URL, relDir, req.Preset, req.Options)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "task": t})
}

func (m *Manager) idReq(w http.ResponseWriter, r *http.Request) (uint, bool) {
	var req struct {
		ID uint `json:"id"`
	}
	if err := httpx.Decode(r, &req); err != nil || req.ID == 0 {
		httpx.Err(w, http.StatusBadRequest, "缺少 id")
		return 0, false
	}
	return req.ID, true
}

func (m *Manager) HandleCancel(w http.ResponseWriter, r *http.Request) {
	id, ok := m.idReq(w, r)
	if !ok {
		return
	}
	if err := m.Cancel(id); err != nil {
		httpx.Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (m *Manager) HandleDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := m.idReq(w, r)
	if !ok {
		return
	}
	_ = m.Cancel(id)
	m.db.Delete(&db.YtdlpTask{}, id)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}
