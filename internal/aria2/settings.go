package aria2

// 下载设置：网页化管理原版 aria2。全局限速/并发通过 RPC 即时应用；
// seed-time、bt-tracker 等任务级选项在添加任务时注入。原版 aria2 不会
// 定时抓取公网 Tracker 列表，因此由 PocketDrive 负责多源更新、缓存和
// 自定义列表合并；DHT/监听端口等启动项由项目维护的 aria2 镜像管理。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"gorm.io/gorm"

	"pocketdrive/internal/db"
	"pocketdrive/internal/httpx"
)

const (
	settingsKey       = "download_settings"
	trackersKey       = "bt_trackers"
	trackersAtKey     = "bt_trackers_at"
	trackersSourceKey = "bt_trackers_source"
	trackersErrorKey  = "bt_trackers_error"

	maxCustomTrackerBytes = 64 << 10
	maxCustomTrackers     = 100
	maxTrackerSourceBytes = 1 << 20
)

// 同一份 trackers_best.txt 的三个官方/公共镜像。按顺序尝试，一个源被
// 墙、DNS 异常或临时 5xx 时自动切下一个；全部失败仍保留上次成功缓存。
var defaultTrackerSources = []string{
	"https://raw.githubusercontent.com/ngosang/trackerslist/master/trackers_best.txt",
	"https://cdn.jsdelivr.net/gh/ngosang/trackerslist@master/trackers_best.txt",
	"https://ngosang.github.io/trackerslist/trackers_best.txt",
}

// 首次启动时公网列表可能还在下载，或三个 HTTP 镜像恰好都不可达。
// 保留少量来自同一 best 列表的启动兜底，避免用户刚登录就添加的磁力
// 完全没有 Tracker；一旦拉取成功，缓存列表会自动替代它们。
var bootstrapTrackers = []string{
	"udp://tracker.publictracker.xyz:6969/announce",
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://open.demonii.com:1337/announce",
}

type Settings struct {
	MaxConcurrent    int    `json:"maxConcurrent"`    // 最大同时下载数
	MaxDownloadLimit string `json:"maxDownloadLimit"` // "0"=不限，如 "5M"
	MaxUploadLimit   string `json:"maxUploadLimit"`
	SeedTimeMin      int    `json:"seedTimeMin"` // 做种分钟数，0=不做种
	TrackerAuto      bool   `json:"trackerAuto"` // 每日自动更新公共 Tracker
	CustomTrackers   string `json:"customTrackers"`
	DefaultDir       string `json:"defaultDir"` // 默认保存目录（相对网盘根）
}

// 2G VPS 的稳妥默认：3 并发、不限速、下载完即停止做种、Tracker 自动更新。
func defaultSettings() Settings {
	return Settings{
		MaxConcurrent:    3,
		MaxDownloadLimit: "0",
		MaxUploadLimit:   "0",
		SeedTimeMin:      0,
		TrackerAuto:      true,
		CustomTrackers:   "",
		DefaultDir:       "",
	}
}

func (m *Manager) getSetting(key string) string {
	var st db.Setting
	if err := m.db.First(&st, "key = ?", key).Error; err != nil {
		return ""
	}
	return st.Value
}

func (m *Manager) Settings() Settings {
	s := defaultSettings()
	if v := m.getSetting(settingsKey); v != "" {
		_ = json.Unmarshal([]byte(v), &s)
	}
	return s
}

func (m *Manager) saveSettings(s Settings) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return m.db.Save(&db.Setting{Key: settingsKey, Value: string(b)}).Error
}

var speedValues = map[string]bool{
	"0": true, "512K": true, "1M": true, "2M": true,
	"5M": true, "10M": true, "20M": true,
}

func validateSettings(s *Settings) error {
	if s.MaxConcurrent < 1 || s.MaxConcurrent > 10 {
		return errors.New("同时下载数须在 1-10 之间")
	}
	if !speedValues[s.MaxDownloadLimit] || !speedValues[s.MaxUploadLimit] {
		return errors.New("限速值不合法")
	}
	if s.SeedTimeMin < 0 || s.SeedTimeMin > 14400 {
		return errors.New("做种时间不合法")
	}
	if len(s.CustomTrackers) > maxCustomTrackerBytes {
		return errors.New("自定义 Tracker 内容过长")
	}
	custom, err := parseTrackerText(s.CustomTrackers, true)
	if err != nil {
		return err
	}
	s.CustomTrackers = strings.Join(custom, "\n")
	s.DefaultDir = cleanRel(s.DefaultDir)
	return nil
}

// applyGlobals 把全局项推给 aria2（不可达时由同步循环在恢复后重试）。
func (m *Manager) applyGlobals() {
	s := m.Settings()
	err := m.c.ChangeGlobalOption(map[string]string{
		"max-concurrent-downloads":   strconv.Itoa(s.MaxConcurrent),
		"max-overall-download-limit": s.MaxDownloadLimit,
		"max-overall-upload-limit":   s.MaxUploadLimit,
	})
	m.globalsApplied.Store(err == nil)
}

// ---- BT Tracker 自动更新与自定义列表 ----

// parseTrackerText 接受换行、逗号、分号或空白分隔的 Tracker。strict=true
// 用于用户输入：遇到非法 URL 直接报出；公网列表则跳过注释和坏行。
func parseTrackerText(raw string, strict bool) ([]string, error) {
	seen := make(map[string]bool)
	list := make([]string, 0)
	itemNo := 0
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r", ""), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.FieldsFunc(line, func(r rune) bool {
			return r == ',' || r == ';' || unicode.IsSpace(r)
		})
		for _, item := range parts {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			itemNo++
			if len(item) > 2048 {
				if strict {
					return nil, fmt.Errorf("第 %d 个 Tracker 地址过长", itemNo)
				}
				continue
			}
			u, err := url.Parse(item)
			scheme := ""
			if u != nil {
				scheme = strings.ToLower(u.Scheme)
			}
			validScheme := scheme == "http" || scheme == "https" || scheme == "udp"
			if err != nil || u == nil || u.Host == "" || !validScheme {
				if strict {
					return nil, fmt.Errorf("第 %d 个 Tracker 无效：%s（仅支持 http/https/udp）", itemNo, item)
				}
				continue
			}
			u.Scheme = scheme
			u.Host = strings.ToLower(u.Host)
			normalized := u.String()
			if !seen[normalized] {
				seen[normalized] = true
				list = append(list, normalized)
				if strict && len(list) > maxCustomTrackers {
					return nil, fmt.Errorf("自定义 Tracker 最多 %d 条", maxCustomTrackers)
				}
			}
		}
	}
	return list, nil
}

func mergeTrackerLists(lists ...[]string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, list := range lists {
		for _, tracker := range list {
			if tracker != "" && !seen[tracker] {
				seen[tracker] = true
				out = append(out, tracker)
			}
		}
	}
	return out
}

func (m *Manager) autoTrackerList() []string {
	list, _ := parseTrackerText(m.getSetting(trackersKey), false)
	if len(list) == 0 {
		return append([]string(nil), bootstrapTrackers...)
	}
	return list
}

func customTrackerList(s Settings) []string {
	list, _ := parseTrackerText(s.CustomTrackers, false)
	return list
}

func (m *Manager) effectiveTrackerList(s Settings) []string {
	var automatic []string
	if s.TrackerAuto {
		automatic = m.autoTrackerList()
	}
	return mergeTrackerLists(automatic, customTrackerList(s))
}

func (m *Manager) effectiveTrackers(s Settings) string {
	return strings.Join(m.effectiveTrackerList(s), ",")
}

func trackerSourceName(source string) string {
	u, err := url.Parse(source)
	if err == nil && u.Host != "" {
		return u.Host
	}
	return source
}

func fetchTrackerSource(ctx context.Context, client *http.Client, source string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "PocketDrive tracker updater")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.New(resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTrackerSourceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxTrackerSourceBytes {
		return nil, errors.New("响应超过 1MB")
	}
	list, _ := parseTrackerText(string(body), false)
	if len(list) < 3 {
		return nil, errors.New("有效 Tracker 少于 3 条")
	}
	return list, nil
}

// updateTrackers 在调用方持有 trackerUpdateMu 时依次尝试多个镜像源。
// 全部失败时只记录错误，不覆盖上次成功缓存。
func (m *Manager) updateTrackers(ctx context.Context) (int, string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	var failures []string
	for _, source := range m.trackerSources {
		if err := ctx.Err(); err != nil {
			failures = append(failures, err.Error())
			break
		}
		list, err := fetchTrackerSource(ctx, client, source)
		if err != nil {
			failures = append(failures, trackerSourceName(source)+": "+err.Error())
			continue
		}
		at := time.Now().Format(time.RFC3339)
		err = m.db.Transaction(func(tx *gorm.DB) error {
			values := []db.Setting{
				{Key: trackersKey, Value: strings.Join(list, "\n")},
				{Key: trackersAtKey, Value: at},
				{Key: trackersSourceKey, Value: source},
				{Key: trackersErrorKey, Value: ""},
			}
			for i := range values {
				if err := tx.Save(&values[i]).Error; err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return 0, "", err
		}
		return len(list), source, nil
	}
	if len(failures) == 0 {
		failures = append(failures, "未配置更新源")
	}
	message := "所有 Tracker 更新源均不可用：" + strings.Join(failures, "；")
	if len(message) > 1000 {
		message = message[:1000]
	}
	_ = m.db.Save(&db.Setting{Key: trackersErrorKey, Value: message}).Error
	return 0, "", errors.New(message)
}

// UpdateTrackers 串行执行一次显式更新。手动点击“立即更新”仍会真正请求
// 公网源；自动刷新则会在拿到同一把锁后再次检查缓存是否已经被别人更新。
func (m *Manager) UpdateTrackers(ctx context.Context) (int, string, error) {
	m.trackerUpdateMu.Lock()
	defer m.trackerUpdateMu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, "", err
	}
	return m.updateTrackers(ctx)
}

func (m *Manager) trackerCacheStale() bool {
	at := m.getSetting(trackersAtKey)
	t, err := time.Parse(time.RFC3339, at)
	return err != nil || time.Since(t) > 24*time.Hour
}

func (m *Manager) refreshTrackersIfStale() {
	if !m.Settings().TrackerAuto || !m.trackerCacheStale() {
		return
	}
	m.trackerUpdateMu.Lock()
	defer m.trackerUpdateMu.Unlock()
	// 可能在等待锁时已经由手动更新或另一轮自动更新刷新过。
	if !m.Settings().TrackerAuto || !m.trackerCacheStale() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	_, _, _ = m.updateTrackers(ctx)
	cancel()
}

func (m *Manager) startTrackerLoop() {
	go func() {
		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			select {
			case <-m.stop:
				return
			case <-timer.C:
				m.refreshTrackersIfStale()
				timer.Reset(time.Hour)
			}
		}
	}()
}

// ---- HTTP handlers ----

func (m *Manager) HandleGetSettings(w http.ResponseWriter, r *http.Request) {
	s := m.Settings()
	automatic := m.autoTrackerList()
	custom := customTrackerList(s)
	effective := m.effectiveTrackerList(s)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"settings":           s,
		"aria2Version":       m.Version(),
		"trackerCount":       len(effective),
		"trackerAutoCount":   len(automatic),
		"trackerCustomCount": len(custom),
		"trackerUpdatedAt":   m.getSetting(trackersAtKey),
		"trackerSource":      m.getSetting(trackersSourceKey),
		"trackerLastError":   m.getSetting(trackersErrorKey),
	})
}

func (m *Manager) HandleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var s Settings
	if err := httpx.Decode(r, &s); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := validateSettings(&s); err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := m.saveSettings(s); err != nil {
		httpx.Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	m.applyGlobals()
	if s.TrackerAuto && m.trackerCacheStale() {
		go m.refreshTrackersIfStale()
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "settings": s})
}

func (m *Manager) HandleUpdateTrackers(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	n, source, err := m.UpdateTrackers(ctx)
	if err != nil {
		httpx.Err(w, http.StatusBadGateway, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"count":     n,
		"source":    source,
		"updatedAt": m.getSetting(trackersAtKey),
	})
}
