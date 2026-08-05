package cloud

// 外部存储的全局开关(与单个策略无关,存在 settings 表里)。

import (
	"encoding/json"
	"net/http"

	"pocketdrive/internal/db"
	"pocketdrive/internal/httpx"
)

const settingsKey = "cloud_settings"

type Settings struct {
	// DavDirect:WebDAV 读 @挂载 里的文件时,302 到预签名 URL,让播放器
	// 直连存储桶——和网页端下载走的是同一条路(files.go HandleDownload)。
	//
	// 关掉则退回 VPS 中转(davfs.go 把桶里的字节拷给客户端):听歌的流量
	// 会走一遍 VPS 的出站。留这个开关是因为少数 WebDAV 客户端不跟随 302
	// (Windows 资源管理器的 WebClient 是典型),直连时会直接报错。
	DavDirect bool `json:"davDirect"`
}

func defaultSettings() Settings { return Settings{DavDirect: true} }

// loadSettings 读库;没有记录或字段缺失时落在默认值上。
func (s *Service) loadSettings() Settings {
	out := defaultSettings()
	var st db.Setting
	if s.db.First(&st, "key = ?", settingsKey).Error == nil && st.Value != "" {
		_ = json.Unmarshal([]byte(st.Value), &out)
	}
	return out
}

func (s *Service) Settings() Settings {
	return Settings{DavDirect: s.davDirect.Load()}
}

// DavDirect 在 WebDAV 的每次 GET 上都要问一遍,所以读内存不查库。
func (s *Service) DavDirect() bool { return s.davDirect.Load() }

func (s *Service) HandleGetSettings(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{"settings": s.Settings()})
}

func (s *Service) HandleSaveSettings(w http.ResponseWriter, r *http.Request) {
	// 以当前值打底:请求体里没带的字段保持原样
	cur := s.Settings()
	if err := httpx.Decode(r, &cur); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	b, err := json.Marshal(cur)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.db.Save(&db.Setting{Key: settingsKey, Value: string(b)}).Error; err != nil {
		httpx.Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.davDirect.Store(cur.DavDirect)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "settings": cur})
}
