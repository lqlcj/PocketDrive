package aria2

// 下载设置:网页化管理 aria2,存 SQLite,变更即时通过
// changeGlobalOption 应用(全局项);seed-time / bt-tracker 属于
// 任务级选项,在 addUri 时注入。DHT、IPv6 等是 aria2 启动项,
// 由 aria2 侧配置(p3terx/aria2-pro 默认已开 DHT),不在此管理。

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pocketdrive/internal/db"
	"pocketdrive/internal/httpx"
)

const (
	settingsKey   = "download_settings"
	trackersKey   = "bt_trackers"
	trackersAtKey = "bt_trackers_at"
	trackersURL   = "https://raw.githubusercontent.com/ngosang/trackerslist/master/trackers_best.txt"
)

type Settings struct {
	MaxConcurrent    int    `json:"maxConcurrent"`    // 最大同时下载数
	MaxDownloadLimit string `json:"maxDownloadLimit"` // "0"=不限,如 "5M"
	MaxUploadLimit   string `json:"maxUploadLimit"`
	SeedTimeMin      int    `json:"seedTimeMin"` // 做种分钟数,0=不做种
	TrackerAuto      bool   `json:"trackerAuto"` // 每日自动更新 BT tracker
	DefaultDir       string `json:"defaultDir"`  // 默认保存目录(相对网盘根)
}

// 2G VPS 的最优默认:3 并发、不限速、下载完即停止做种、tracker 自动更新。
// 默认保存目录是网盘根目录——早期版本默认 "downloads",但网盘本身就是
// 一棵目录树,没必要再造一个固定的收纳夹。
func defaultSettings() Settings {
	return Settings{
		MaxConcurrent:    3,
		MaxDownloadLimit: "0",
		MaxUploadLimit:   "0",
		SeedTimeMin:      0,
		TrackerAuto:      true,
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

var speedValues = map[string]bool{"0": true, "512K": true, "1M": true, "2M": true, "5M": true, "10M": true, "20M": true}

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
	s.DefaultDir = cleanRel(s.DefaultDir)
	return nil
}

// applyGlobals 把全局项推给 aria2(aria2 不可达时静默,恢复后由
// sync 重试)。
func (m *Manager) applyGlobals() {
	s := m.Settings()
	err := m.c.ChangeGlobalOption(map[string]string{
		"max-concurrent-downloads":   strconv.Itoa(s.MaxConcurrent),
		"max-overall-download-limit": s.MaxDownloadLimit,
		"max-overall-upload-limit":   s.MaxUploadLimit,
	})
	m.globalsApplied.Store(err == nil)
}

// ---- BT tracker 自动更新 ----

func (m *Manager) trackers() string {
	return m.getSetting(trackersKey)
}

// UpdateTrackers 拉取 trackerslist(best),存库供磁力任务注入。
func (m *Manager) UpdateTrackers(ctx context.Context) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trackersURL, nil)
	if err != nil {
		return 0, err
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, errors.New("tracker 列表源返回 " + resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, err
	}
	var list []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			list = append(list, line)
		}
	}
	if len(list) == 0 {
		return 0, errors.New("tracker 列表为空")
	}
	m.db.Save(&db.Setting{Key: trackersKey, Value: strings.Join(list, ",")})
	m.db.Save(&db.Setting{Key: trackersAtKey, Value: time.Now().Format(time.RFC3339)})
	return len(list), nil
}

func (m *Manager) startTrackerLoop() {
	go func() {
		for {
			if m.Settings().TrackerAuto {
				at := m.getSetting(trackersAtKey)
				stale := true
				if t, err := time.Parse(time.RFC3339, at); err == nil {
					stale = time.Since(t) > 24*time.Hour
				}
				if stale {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					_, _ = m.UpdateTrackers(ctx)
					cancel()
				}
			}
			time.Sleep(time.Hour)
		}
	}()
}

// ---- HTTP handlers ----

func (m *Manager) HandleGetSettings(w http.ResponseWriter, r *http.Request) {
	s := m.Settings()
	trackerCount := 0
	if t := m.trackers(); t != "" {
		trackerCount = strings.Count(t, ",") + 1
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"settings":         s,
		"trackerCount":     trackerCount,
		"trackerUpdatedAt": m.getSetting(trackersAtKey),
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
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "settings": s})
}

func (m *Manager) HandleUpdateTrackers(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	n, err := m.UpdateTrackers(ctx)
	if err != nil {
		httpx.Err(w, http.StatusBadGateway, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "count": n})
}
