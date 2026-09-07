package aria2

import (
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
	"unicode/utf8"

	"pocketdrive/internal/db"
	"pocketdrive/internal/httpx"
)

const customTrackersKey = "bt_trackers_custom"
const maxTrackerBytes = 1 << 20

type customTrackerList struct {
	List      string `json:"list"`
	UpdatedAt string `json:"updatedAt"`
}

func (m *Manager) customTrackers() customTrackerList {
	var custom customTrackerList
	_ = json.Unmarshal([]byte(m.getSetting(customTrackersKey)), &custom)
	return custom
}

func parseTrackers(body []byte) (string, error) {
	if !utf8.Valid(body) {
		return "", errors.New("请使用 UTF-8 编码的 Tracker 文本文件")
	}
	var list []string
	seen := make(map[string]bool)
	for i, line := range strings.Split(strings.TrimPrefix(string(body), "\uFEFF"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		u, err := url.Parse(line)
		if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" ||
			(u.Scheme != "udp" && u.Scheme != "http" && u.Scheme != "https") ||
			strings.Contains(line, ",") || strings.IndexFunc(line, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
			return "", fmt.Errorf("第 %d 行不是有效的 udp/http/https Tracker 地址", i+1)
		}
		if port := u.Port(); port != "" || u.Scheme == "udp" {
			n, err := strconv.Atoi(port)
			if err != nil || n < 1 || n > 65535 {
				return "", fmt.Errorf("第 %d 行的 Tracker 端口无效", i+1)
			}
		}
		if !seen[line] {
			seen[line] = true
			list = append(list, line)
		}
	}
	if len(list) == 0 {
		return "", errors.New("文件中没有有效的 Tracker 地址")
	}
	return strings.Join(list, ","), nil
}

func (m *Manager) HandleImportTrackers(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxTrackerBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			httpx.Err(w, http.StatusRequestEntityTooLarge, "Tracker 文件不能超过 1MB")
		} else {
			httpx.Err(w, http.StatusBadRequest, "读取 Tracker 文件失败")
		}
		return
	}
	list, err := parseTrackers(body)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	value, _ := json.Marshal(customTrackerList{List: list, UpdatedAt: time.Now().Format(time.RFC3339)})
	if err := m.db.Save(&db.Setting{Key: customTrackersKey, Value: string(value)}).Error; err != nil {
		httpx.Err(w, http.StatusInternalServerError, "保存 Tracker 列表失败")
		return
	}
	m.HandleGetSettings(w, r)
}

func (m *Manager) HandleResetTrackers(w http.ResponseWriter, r *http.Request) {
	if err := m.db.Delete(&db.Setting{}, "key = ?", customTrackersKey).Error; err != nil {
		httpx.Err(w, http.StatusInternalServerError, "恢复默认 Tracker 失败")
		return
	}
	m.HandleGetSettings(w, r)
}
