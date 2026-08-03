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
)

const logTailLines = 40

var (
	rePercent = regexp.MustCompile(`\[download\]\s+([0-9.]+)%`)
	reDest    = regexp.MustCompile(`(?:Destination:|Merging formats into ")\s*(.+?)"?\s*$`)
)

type Manager struct {
	db      *gorm.DB
	bin     string
	dataDir string
	queue   chan uint

	mu      sync.Mutex
	cancels map[uint]context.CancelFunc

	verMu      sync.Mutex
	version    string
	verChecked time.Time
}

func NewManager(gdb *gorm.DB, bin, dataDir string) *Manager {
	m := &Manager{
		db:      gdb,
		bin:     bin,
		dataDir: dataDir,
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
}

// presets 是服务端固定白名单:用户只能选 key,参数模板不可注入
var presets = map[string][]string{
	"video_best": {"-f", "bv*[ext=mp4]+ba[ext=m4a]/b[ext=mp4]/b", "--merge-output-format", "mp4"},
	"video_1080": {"-f", "bv*[ext=mp4][height<=1080]+ba[ext=m4a]/b[ext=mp4][height<=1080]/b[height<=1080]/b", "--merge-output-format", "mp4"},
	"video_720":  {"-f", "bv*[ext=mp4][height<=720]+ba[ext=m4a]/b[ext=mp4][height<=720]/b[height<=720]/b", "--merge-output-format", "mp4"},
	"video_480":  {"-f", "bv*[ext=mp4][height<=480]+ba[ext=m4a]/b[ext=mp4][height<=480]/b[height<=480]/b", "--merge-output-format", "mp4"},
	// 音频:多数站点音轨本就是 m4a,仅 remux;mp3 需转码但音频转码很快
	"audio_m4a": {"-f", "bestaudio", "-x", "--audio-format", "m4a"},
	"audio_mp3": {"-f", "bestaudio", "-x", "--audio-format", "mp3", "--audio-quality", "0"},
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
	args := []string{"--newline", "--no-playlist", "--no-warnings",
		"-o", filepath.Join(absDir, "%(title)s.%(ext)s")}
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
		m.finish(&t, "error", lastLines(t.LogTail, 5))
	default:
		t.Progress = 100
		m.finish(&t, "done", "")
	}
}

// pump reads merged stdout/stderr, tracking progress and a log tail.
func (m *Manager) pump(t *db.YtdlpTask, r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 256*1024)
	var lines []string
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
		if mm := rePercent.FindStringSubmatch(line); mm != nil {
			if v, err := strconv.ParseFloat(mm[1], 64); err == nil {
				t.Progress = v
			}
		}
		if mm := reDest.FindStringSubmatch(line); mm != nil {
			t.Title = path.Base(strings.ReplaceAll(mm[1], "\\", "/"))
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

// HandleUpdate runs yt-dlp -U (self-update; works with the standalone
// binary — package-manager installs will just report and exit).
func (m *Manager) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	if ok, _ := m.Available(); !ok {
		httpx.Err(w, http.StatusBadRequest, "yt-dlp 不可用")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, m.bin, "-U").CombinedOutput()
	m.verMu.Lock()
	m.verChecked = time.Time{} // 让下次 Available() 重新读版本
	m.verMu.Unlock()
	httpx.JSON(w, http.StatusOK, map[string]any{
		"ok":     err == nil,
		"output": string(out),
	})
}
