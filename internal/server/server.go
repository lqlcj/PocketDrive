package server

import (
	"net/http"

	"pocketdrive/internal/aria2"
	"pocketdrive/internal/auth"
	"pocketdrive/internal/config"
	"pocketdrive/internal/dav"
	"pocketdrive/internal/files"
	"pocketdrive/internal/httpx"
	"pocketdrive/internal/index"
	"pocketdrive/internal/share"
	"pocketdrive/internal/storage"
	"pocketdrive/internal/thumbs"
	"pocketdrive/internal/trash"
	"pocketdrive/internal/ytdlp"
	"pocketdrive/web"
)

type Deps struct {
	Auth    *auth.Service
	Files   *files.Service
	Storage *storage.Service
	Aria2   *aria2.Manager
	Ytdlp   *ytdlp.Manager
	Share   *share.Service
	Thumbs  *thumbs.Service
	Trash   *trash.Service
	Index   *index.Service
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
	mux.HandleFunc("GET /d/{token}", d.Share.HandleDirect)

	// authenticated API
	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/auth/me", d.Auth.HandleMe)
	api.HandleFunc("POST /api/v1/auth/password", d.Auth.HandleChangePassword)
	api.HandleFunc("POST /api/v1/auth/profile", d.Auth.HandleProfile)

	api.HandleFunc("GET /api/v1/files", d.Files.HandleList)
	api.HandleFunc("POST /api/v1/files/mkdir", d.Files.HandleMkdir)
	api.HandleFunc("POST /api/v1/files/upload", d.Files.HandleUpload)
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
		})
	})

	api.HandleFunc("GET /api/v1/downloads", d.Aria2.HandleList)
	api.HandleFunc("POST /api/v1/downloads", d.Aria2.HandleAdd)
	api.HandleFunc("POST /api/v1/downloads/pause", d.Aria2.HandlePause)
	api.HandleFunc("POST /api/v1/downloads/unpause", d.Aria2.HandleUnpause)
	api.HandleFunc("POST /api/v1/downloads/remove", d.Aria2.HandleRemove)

	api.HandleFunc("GET /api/v1/ytdlp", d.Ytdlp.HandleList)
	api.HandleFunc("POST /api/v1/ytdlp", d.Ytdlp.HandleAdd)
	api.HandleFunc("POST /api/v1/ytdlp/cancel", d.Ytdlp.HandleCancel)
	api.HandleFunc("POST /api/v1/ytdlp/delete", d.Ytdlp.HandleDelete)
	api.HandleFunc("POST /api/v1/ytdlp/update", d.Ytdlp.HandleUpdate)

	mux.Handle("/api/v1/", d.Auth.Middleware(api))

	// WebDAV: whole data dir, Basic Auth (same admin account)
	davHandler := d.Auth.BasicAuth(dav.Handler(cfg.DataDir))
	mux.Handle("/dav/", davHandler)
	mux.Handle("/dav", davHandler)

	// embedded SPA
	mux.Handle("/", web.Handler())

	return &http.Server{
		Addr:    cfg.Addr,
		Handler: auth.CSRF(mux),
	}
}
