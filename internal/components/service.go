package components

// HTTP 层:设置页的「组件状态」卡片。三个组件各自显示版本,并如实
// 说明各自该怎么升级——只有 yt-dlp 能在网页里点一下就更新,另外两个
// 得在服务器上 docker compose pull,这一点必须写在界面上,不然用户
// 到要更新的那天无从下手。

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

	// aria2 跑在自己的容器里,版本和可达性都只能问它自己
	aria2Version func() string
	aria2OK      func() bool
}

func NewService(m *Manager, gdb *gorm.DB, aria2Version func() string,
	aria2OK func() bool) *Service {
	return &Service{m: m, db: gdb, aria2Version: aria2Version, aria2OK: aria2OK}
}

type view struct {
	Info
	Title       string `json:"title"`
	Note        string `json:"note"`
	LastUpdated string `json:"lastUpdated"`
	// Running 仅对 aria2 有意义:它在另一个容器里,可能连不上
	Running bool `json:"running"`
	// UpdateHint 是不能在网页里升级时,替代按钮显示的一句话
	UpdateHint string `json:"updateHint"`
}

var meta = map[Kind]struct{ title, note string }{
	Ytdlp:  {"yt-dlp", "yt下载(视频站点解析)"},
	Aria2:  {"aria2", "离线下载(直链 / 磁力 / BT)"},
	FFmpeg: {"ffmpeg", "视频封面抽帧、yt下载的音视频合并"},
}

// updateHint 用一句话说明组件的升级渠道,直接显示在网页上。
func updateHint(c Channel) string {
	switch c {
	case ChanSidecar:
		return "随 aria2 容器更新"
	case ChanImage:
		return "随 PocketDrive 镜像更新"
	case ChanSystem:
		return "由本机 PATH 决定"
	}
	return ""
}

func (s *Service) HandleList(w http.ResponseWriter, r *http.Request) {
	// 查上游版本要联网,给个上限免得前端一直转
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()

	out := make([]view, 0, len(meta))
	for _, k := range []Kind{Ytdlp, Aria2, FFmpeg} {
		info := s.m.Status(ctx, k)
		v := view{
			Info: info, Title: meta[k].title, Note: meta[k].note,
			Running: true, UpdateHint: updateHint(info.Channel),
		}
		if k == Aria2 {
			// 不在本容器内:版本走 RPC,连不上就什么也报不出来
			if s.aria2Version != nil {
				v.Version = s.aria2Version()
				v.Installed = v.Version != ""
			}
			if s.aria2OK != nil {
				v.Running = s.aria2OK()
			}
		}
		var st db.Setting
		if s.db.First(&st, "key = ?", settingKey(k)).Error == nil {
			v.LastUpdated = st.Value
		}
		out = append(out, v)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"components": out,
		// 有没有任何一个组件能在网页里升级(本机开发时没有)
		"managed": s.m.Managed(Ytdlp),
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
	httpx.JSON(w, http.StatusOK, map[string]any{
		"ok": true, "version": s.m.Version(k),
	})
}
