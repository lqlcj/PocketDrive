package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"pocketdrive/internal/archive"
	"pocketdrive/internal/aria2"
	"pocketdrive/internal/auth"
	"pocketdrive/internal/cloud"
	"pocketdrive/internal/config"
	"pocketdrive/internal/db"
	"pocketdrive/internal/files"
	"pocketdrive/internal/icons"
	"pocketdrive/internal/index"
	"pocketdrive/internal/logs"
	"pocketdrive/internal/server"
	"pocketdrive/internal/share"
	"pocketdrive/internal/storage"
	"pocketdrive/internal/thumbs"
	"pocketdrive/internal/trash"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// 错误日志和 DB 放一起(容器里就是 /config 卷),重启不丢。
	// 只记 error、每天清空,详见 internal/logs。
	logs.Init(filepath.Join(filepath.Dir(cfg.DBPath), "logs"))

	// 导入进来的配置库在这里顶替正式库——必须赶在 db.Open 之前
	if err := archive.RestorePendingImport(cfg.DBPath); err != nil {
		log.Fatalf("恢复导入的配置库: %v", err)
	}

	gdb, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}

	authSvc, err := auth.New(gdb, cfg.AdminUser, cfg.AdminPassword,
		filepath.Dir(cfg.DBPath))
	if err != nil {
		log.Fatalf("init auth: %v", err)
	}

	cloudSvc := cloud.New(gdb)

	fileSvc, err := files.New(cfg.DataDir, filepath.Join(filepath.Dir(cfg.DBPath), "uploads"), cloudSvc, gdb)
	if err != nil {
		log.Fatalf("init files: %v", err)
	}
	fileSvc.StartCleanup()

	storageSvc := storage.New(cfg.DataDir, fileSvc.Root().FS(), gdb)
	// 本机容量检查的回调(storage 依赖 files 的 FS 构造,反过来再由这里回填)
	fileSvc.SetLocalSpace(storageSvc)

	aria2Mgr := aria2.NewManager(gdb,
		aria2.NewClient(cfg.Aria2RPC, cfg.Aria2Secret), cfg.Aria2DataDir, cfg.DataDir)
	aria2Mgr.Start()

	// 缩略图缓存放 DB 同级目录,不会出现在网盘/WebDAV 里
	thumbSvc := thumbs.New(fileSvc, filepath.Join(filepath.Dir(cfg.DBPath), "thumbs"),
		func() string { return "ffmpeg" })
	shareSvc := share.New(gdb, fileSvc, thumbSvc, cloudSvc)

	trashSvc := trash.New(gdb, fileSvc, cloudSvc)
	trashSvc.Start()

	indexSvc := index.New(fileSvc.Root().FS())
	iconsSvc := icons.New(gdb)
	archiveSvc := archive.New(gdb, fileSvc, cloudSvc, authSvc, cfg.DBPath, config.Version)
	srv := server.New(cfg, server.Deps{
		Auth:    authSvc,
		Files:   fileSvc,
		Storage: storageSvc,
		Aria2:   aria2Mgr,
		Share:   shareSvc,
		Thumbs:  thumbSvc,
		Trash:   trashSvc,
		Index:   indexSvc,
		Icons:   iconsSvc,
		Cloud:   cloudSvc,
		Archive: archiveSvc,
	})

	// docker stop 会发 SIGTERM:把正在传的请求收完再退,别让用户的
	// 上传/下载被硬切
	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		log.Print("收到停止信号,正在关闭…")
		shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutCtx); err != nil {
			log.Printf("关闭超时,直接退出: %v", err)
		}
	}()

	log.Printf("PocketDrive %s listening on %s (data: %s)", config.Version, cfg.Addr, cfg.DataDir)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
