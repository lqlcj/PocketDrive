package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
	"pocketdrive/internal/server"
	"pocketdrive/internal/share"
	"pocketdrive/internal/storage"
	"pocketdrive/internal/thumbs"
	"pocketdrive/internal/trash"
	"pocketdrive/internal/watch"
)

// internalRoot 决定内部目录(头像、分片暂存、缩略图缓存)放在哪。
//
// 正常情况是数据库同级目录(docker 里的 /config):不在网盘里,既不会
// 出现在文件列表和 WebDAV 中,也不会被自动清理波及。
//
// 但 POCKETDRIVE_DB 是可以被指进网盘的(比如 /data/pocketdrive.db)。
// 那样 uploads/ 就成了用户看得见、能往里放东西的普通文件夹,而分片暂存
// 带 24 小时自动清理——放进去的东西会隔天不翼而飞。检测到这种配置就改用
// 网盘里的隐藏目录 .pocketdrive/(点开头,列表与用量统计都会跳过)。
func internalRoot(dataDir, dbPath string) string {
	dbDir := filepath.Dir(dbPath)
	absData, err1 := filepath.Abs(dataDir)
	absDB, err2 := filepath.Abs(dbDir)
	if err1 != nil || err2 != nil {
		return dbDir
	}
	rel, err := filepath.Rel(absData, absDB)
	if err != nil || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return dbDir // 在网盘之外,正常情况
	}
	p := filepath.Join(absData, ".pocketdrive")
	log.Printf("警告:数据库位于网盘目录内(%s),内部目录改用 %s。"+
		"建议把 POCKETDRIVE_DB 指到网盘之外(如 /config/pocketdrive.db)", absDB, p)
	return p
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	gdb, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}

	// 头像、分片暂存、缩略图缓存这些内部目录的落脚点
	intDir := internalRoot(cfg.DataDir, cfg.DBPath)

	authSvc, err := auth.New(gdb, cfg.AdminUser, cfg.AdminPassword, intDir)
	if err != nil {
		log.Fatalf("init auth: %v", err)
	}

	cloudSvc := cloud.New(gdb)

	fileSvc, err := files.New(cfg.DataDir, filepath.Join(intDir, "uploads"), cloudSvc, gdb)
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

	// 缩略图缓存放内部目录,不会出现在网盘/WebDAV 里
	thumbSvc := thumbs.New(fileSvc, filepath.Join(intDir, "thumbs"),
		func() string { return "ffmpeg" })
	shareSvc := share.New(gdb, fileSvc, thumbSvc, cloudSvc)

	trashSvc := trash.New(gdb, fileSvc, cloudSvc)
	trashSvc.Start()

	indexSvc := index.New(fileSvc.Root().FS())
	iconsSvc := icons.New(gdb)
	archiveSvc := archive.New(gdb, fileSvc, cloudSvc)

	// 网盘目录哨兵:东西在 PocketDrive 之外被删掉时,日志里至少留个话
	watch.New(fileSvc.Root().FS(), filepath.Join(intDir, "sentinel.json")).Start()
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
