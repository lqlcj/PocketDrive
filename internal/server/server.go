package server

import (
	"context"
	"net/http"
	"time"

	"pocketdrive/internal/archive"
	"pocketdrive/internal/aria2"
	"pocketdrive/internal/auth"
	"pocketdrive/internal/cloud"
	"pocketdrive/internal/components"
	"pocketdrive/internal/config"
	"pocketdrive/internal/dav"
	"pocketdrive/internal/files"
	"pocketdrive/internal/httpx"
	"pocketdrive/internal/icons"
	"pocketdrive/internal/index"
	"pocketdrive/internal/share"
	"pocketdrive/internal/storage"
	"pocketdrive/internal/thumbs"
	"pocketdrive/internal/trash"
	"pocketdrive/internal/updater"
	"pocketdrive/internal/ytdlp"
	"pocketdrive/web"
)

type Deps struct {
	Auth       *auth.Service
	Files      *files.Service
	Storage    *storage.Service
	Aria2      *aria2.Manager
	Ytdlp      *ytdlp.Manager
	Share      *share.Service
	Thumbs     *thumbs.Service
	Trash      *trash.Service
	Index      *index.Service
	Icons      *icons.Service
	Cloud      *cloud.Service
	Archive    *archive.Service
	Components *components.Service
	Updater    *updater.Service
}

func New(cfg *config.Config, d Deps) *http.Server {
	mux := http.NewServeMux()

	// public
	mux.HandleFunc("GET /api/v1/ping", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{"status": "ok", "version": config.Version})
	})
	mux.HandleFunc("POST /api/v1/auth/login", d.Auth.HandleLogin)
	mux.HandleFunc("POST /api/v1/auth/logout", d.Auth.HandleLogout)

	// 公开分享(免登录)
	mux.HandleFunc("GET /api/v1/public/share/{token}", d.Share.HandleInfo)
	mux.HandleFunc("GET /api/v1/public/share/{token}/download", d.Share.HandleDownload)
	mux.HandleFunc("GET /api/v1/public/share/{token}/thumb", d.Share.HandleThumb)
	// 直链两种形式等价:带文件名段的 URL 自带真实后缀,播放器/下载工具按后缀识别
	mux.HandleFunc("GET /d/{token}", d.Share.HandleDirect)
	mux.HandleFunc("GET /d/{token}/{name}", d.Share.HandleDirect)
	// 头像要在登录页(未登录)也能显示,和分享一样走公开路由
	mux.HandleFunc("GET /api/v1/public/avatar", d.Auth.HandleAvatar)

	// authenticated API
	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/auth/me", d.Auth.HandleMe)
	api.HandleFunc("POST /api/v1/auth/password", d.Auth.HandleChangePassword)
	api.HandleFunc("POST /api/v1/auth/profile", d.Auth.HandleProfile)
	// 自定义头像:存在配置目录,不出现在网盘/WebDAV 里
	api.HandleFunc("POST /api/v1/auth/avatar", d.Auth.HandleAvatarUpload)
	api.HandleFunc("POST /api/v1/auth/avatar/delete", d.Auth.HandleAvatarDelete)

	api.HandleFunc("GET /api/v1/files", d.Files.HandleList)
	api.HandleFunc("POST /api/v1/files/mkdir", d.Files.HandleMkdir)
	api.HandleFunc("POST /api/v1/files/upload", d.Files.HandleUpload)
	api.HandleFunc("POST /api/v1/files/upload/init", d.Files.HandleUploadInit)
	api.HandleFunc("POST /api/v1/files/upload/chunk", d.Files.HandleUploadChunk)
	api.HandleFunc("POST /api/v1/files/upload/complete", d.Files.HandleUploadComplete)
	api.HandleFunc("GET /api/v1/files/download", d.Files.HandleDownload)
	api.HandleFunc("GET /api/v1/files/content", d.Files.HandleContent)
	api.HandleFunc("POST /api/v1/files/rename", d.Files.HandleRename)
	api.HandleFunc("POST /api/v1/files/move", d.Files.HandleMove)
	api.HandleFunc("POST /api/v1/files/write", d.Files.HandleWrite)
	// 删除 = 进回收站
	api.HandleFunc("POST /api/v1/files/delete", d.Trash.HandleDeleteToTrash)
	api.HandleFunc("GET /api/v1/files/thumb", d.Thumbs.HandleThumb)

	api.HandleFunc("GET /api/v1/trash", d.Trash.HandleList)
	api.HandleFunc("POST /api/v1/trash/restore", d.Trash.HandleRestore)
	api.HandleFunc("POST /api/v1/trash/delete", d.Trash.HandlePermDelete)
	api.HandleFunc("POST /api/v1/trash/empty", d.Trash.HandleEmpty)

	api.HandleFunc("GET /api/v1/search", d.Index.HandleSearch)
	api.HandleFunc("GET /api/v1/category", d.Index.HandleCategory)
	api.HandleFunc("GET /api/v1/stats", d.Index.HandleStats)

	api.HandleFunc("GET /api/v1/icons", d.Icons.HandleList)
	api.HandleFunc("POST /api/v1/icons", d.Icons.HandleSet)

	// 存储策略(S3/R2 挂载)
	api.HandleFunc("GET /api/v1/storages", d.Cloud.HandleList)
	api.HandleFunc("POST /api/v1/storages", d.Cloud.HandleSave)
	api.HandleFunc("POST /api/v1/storages/delete", d.Cloud.HandleDelete)
	api.HandleFunc("POST /api/v1/storages/test", d.Cloud.HandleTest)
	// 外部存储全局开关(目前只有 WebDAV 是否直连存储桶)
	api.HandleFunc("GET /api/v1/storages/settings", d.Cloud.HandleGetSettings)
	api.HandleFunc("POST /api/v1/storages/settings", d.Cloud.HandleSaveSettings)
	api.HandleFunc("GET /api/v1/auth/webdav-settings", d.Auth.HandleGetWebDAVSettings)
	api.HandleFunc("POST /api/v1/auth/webdav-settings", d.Auth.HandleSaveWebDAVSettings)

	api.HandleFunc("GET /api/v1/downloads/settings", d.Aria2.HandleGetSettings)
	api.HandleFunc("POST /api/v1/downloads/settings", d.Aria2.HandleSaveSettings)
	api.HandleFunc("POST /api/v1/downloads/trackers/update", d.Aria2.HandleUpdateTrackers)

	api.HandleFunc("GET /api/v1/shares", d.Share.HandleList)
	api.HandleFunc("POST /api/v1/shares", d.Share.HandleCreate)
	api.HandleFunc("POST /api/v1/shares/delete", d.Share.HandleDelete)

	api.HandleFunc("GET /api/v1/storage", func(w http.ResponseWriter, r *http.Request) {
		du, err := d.Storage.DiskUsage()
		if err != nil {
			httpx.Err(w, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{
			"disk":   du,
			"recent": d.Storage.Recent(),
			// 挂载的外部存储各自的用量,首次访问返回 pending 并后台统计
			"mounts": d.Cloud.UsageAll(),
			// 本机网盘目录的用量与上限,首次访问返回 pending 并后台统计
			"local": d.Storage.Usage(),
		})
	})
	// 本机容量上限,0 = 不限(与 S3 挂载的 quota 同一套语义)
	api.HandleFunc("POST /api/v1/storage/quota", d.Storage.HandleSetQuota)

	// 档案:压缩/解压是异步任务;整盘导出导入用于换 VPS 迁移
	api.HandleFunc("GET /api/v1/archive", d.Archive.HandleList)
	api.HandleFunc("POST /api/v1/archive/compress", d.Archive.HandleCompress)
	api.HandleFunc("POST /api/v1/archive/extract", d.Archive.HandleExtract)
	api.HandleFunc("POST /api/v1/archive/delete", d.Archive.HandleDelete)
	api.HandleFunc("GET /api/v1/admin/export", d.Archive.HandleExport)
	api.HandleFunc("POST /api/v1/admin/import", d.Archive.HandleImport)

	// 组件状态与升级:三个组件都装在 config 卷里,可各自在网页升级
	api.HandleFunc("GET /api/v1/components", d.Components.HandleList)
	api.HandleFunc("POST /api/v1/components/install", d.Components.HandleInstall)
	api.HandleFunc("GET /api/v1/system/update", func(w http.ResponseWriter, r *http.Request) {
		enabled := d.Updater != nil && d.Updater.Enabled()
		httpx.JSON(w, http.StatusOK, map[string]any{"enabled": enabled, "version": config.Version})
	})
	api.HandleFunc("POST /api/v1/system/update", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Password string `json:"password"`
		}
		if err := httpx.Decode(r, &req); err != nil || req.Password == "" || !d.Auth.Check(d.Auth.User(), req.Password) {
			httpx.Err(w, http.StatusUnauthorized, "当前密码不正确")
			return
		}
		if d.Updater == nil {
			httpx.Err(w, http.StatusNotImplemented, "在线升级未配置")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if err := d.Updater.Trigger(ctx); err != nil {
			httpx.Err(w, http.StatusBadGateway, err.Error())
			return
		}
		httpx.JSON(w, http.StatusAccepted, map[string]any{"ok": true, "version": config.Version})
	})

	// 错误日志:只记 error、每天清空,供出问题时回看
	api.HandleFunc("GET /api/v1/logs", handleLogs)
	api.HandleFunc("POST /api/v1/logs/clear", handleLogsClear)
	api.HandleFunc("GET /api/v1/logs/download", handleLogsDownload)

	api.HandleFunc("GET /api/v1/downloads", d.Aria2.HandleList)
	api.HandleFunc("POST /api/v1/downloads", d.Aria2.HandleAdd)
	api.HandleFunc("POST /api/v1/downloads/torrent", d.Aria2.HandleAddTorrent)
	api.HandleFunc("GET /api/v1/downloads/{gid}/files", d.Aria2.HandleTorrentFiles)
	api.HandleFunc("POST /api/v1/downloads/{gid}/select", d.Aria2.HandleSelectFiles)
	api.HandleFunc("POST /api/v1/downloads/pause", d.Aria2.HandlePause)
	api.HandleFunc("POST /api/v1/downloads/unpause", d.Aria2.HandleUnpause)
	api.HandleFunc("POST /api/v1/downloads/remove", d.Aria2.HandleRemove)

	api.HandleFunc("GET /api/v1/ytdlp", d.Ytdlp.HandleList)
	api.HandleFunc("POST /api/v1/ytdlp", d.Ytdlp.HandleAdd)
	api.HandleFunc("POST /api/v1/ytdlp/cancel", d.Ytdlp.HandleCancel)
	api.HandleFunc("POST /api/v1/ytdlp/delete", d.Ytdlp.HandleDelete)
	// cookies / 代理 / player client:机房 IP 被 YouTube 判定为机器人时要用
	api.HandleFunc("GET /api/v1/ytdlp/settings", d.Ytdlp.HandleGetSettings)
	api.HandleFunc("POST /api/v1/ytdlp/settings", d.Ytdlp.HandleSaveSettings)
	api.HandleFunc("POST /api/v1/ytdlp/cookies", d.Ytdlp.HandleSetCookies)

	mux.Handle("/api/v1/", d.Auth.Middleware(api))

	// WebDAV: whole data dir + cloud mounts, Basic Auth (same admin account)
	davHandler := d.Auth.BasicAuth(dav.Handler(cfg.DataDir, d.Cloud))
	mux.Handle("/dav/", davHandler)
	mux.Handle("/dav", davHandler)

	// embedded SPA
	mux.Handle("/", web.Handler())

	return &http.Server{
		Addr: cfg.Addr,
		// observe 在最外层:CSRF 自己也可能 panic 或返回 5xx
		Handler: observe(auth.CSRF(mux)),
	}
}
