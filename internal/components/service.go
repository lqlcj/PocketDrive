package components

// HTTP 层:设置页的「组件状态」卡片。三个组件各自显示版本、是否有新版,
// 并能单独点击升级。

import (
	"context"
	"net/http"
	"time"

	"gorm.io/gorm"

	"pocketdrive/internal/db"
	"pocketdrive/internal/httpx"
)

// settingKey 记录每个组件上次成功升级的时间。
func settingKey(k Kind) string { return "component_updated_" + string(k) }

type Service struct {
	m  *Manager
	db *gorm.DB

	// aria2 升级后要重启子进程,新二进制才会生效;连的是外部 aria2 时为 nil
	restartAria2 func(context.Context) error
	// aria2 当前是否连得上(展示用)
	aria2OK func() bool
}

func NewService(m *Manager, gdb *gorm.DB, restartAria2 func(context.Context) error,
	aria2OK func() bool) *Service {
	return &Service{m: m, db: gdb, restartAria2: restartAria2, aria2OK: aria2OK}
}

type view struct {
	Info
	Title       string `json:"title"`
	Note        string `json:"note"`
	LastUpdated string `json:"lastUpdated"`
	// Running 仅对 aria2 有意义:装好了但进程没连上也算不可用
	Running bool `json:"running"`
}

var meta = map[Kind]struct{ title, note string }{
	Ytdlp:  {"yt-dlp", "yt下载(视频站点解析)"},
	Aria2:  {"aria2", "离线下载(直链 / 磁力 / BT)"},
	FFmpeg: {"ffmpeg", "视频封面抽帧、yt下载的音视频合并"},
}

func (s *Service) HandleList(w http.ResponseWriter, r *http.Request) {
	// 查上游版本要联网,给个上限免得前端一直转
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()

	out := make([]view, 0, 3)
	for _, k := range []Kind{Ytdlp, Aria2, FFmpeg} {
		info := s.m.Status(ctx, k)
		v := view{Info: info, Title: meta[k].title, Note: meta[k].note, Running: true}
		if k == Aria2 && s.aria2OK != nil {
			v.Running = s.aria2OK()
		}
		var st db.Setting
		if s.db.First(&st, "key = ?", settingKey(k)).Error == nil {
			v.LastUpdated = st.Value
		}
		out = append(out, v)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"components": out,
		"managed":    s.m.Managed(),
	})
}

func (s *Service) HandleInstall(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind string `json:"kind"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	k := Kind(req.Kind)
	if _, ok := meta[k]; !ok {
		httpx.Err(w, http.StatusBadRequest, "未知组件")
		return
	}
	// 下载可能要几分钟,不挂在请求上下文上会更稳,但用户要等结果,
	// 这里仍同步返回,只把超时放宽
	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()

	if err := s.m.Install(ctx, k); err != nil {
		httpx.Err(w, http.StatusBadGateway, err.Error())
		return
	}
	s.db.Save(&db.Setting{
		Key:   settingKey(k),
		Value: time.Now().Format(time.RFC3339),
	})

	// aria2 换了二进制要重启进程才生效
	restarted := false
	if k == Aria2 && s.restartAria2 != nil {
		if err := s.restartAria2(ctx); err == nil {
			restarted = true
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"ok": true, "version": s.m.Version(k), "restarted": restarted,
	})
}
