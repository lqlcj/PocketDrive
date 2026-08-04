package config

import (
	"os"
)

const Version = "0.5.0"

type Config struct {
	// Addr is the HTTP listen address, e.g. ":8080".
	Addr string
	// DataDir is the root of the drive; all file operations, WebDAV,
	// aria2 and yt-dlp downloads live under this single directory.
	DataDir string
	// DBPath is the SQLite database file (kept outside DataDir so it
	// doesn't show up in the drive / WebDAV listing).
	DBPath string

	// AdminUser / AdminPassword seed the single admin account on first
	// run; afterwards credentials live in the database.
	AdminUser     string
	AdminPassword string

	// Aria2RPC 指向 aria2 的 JSON-RPC 接口。aria2 始终跑在独立容器里
	// (docker compose 的 aria2 服务),PocketDrive 只做客户端。
	Aria2RPC    string
	Aria2Secret string
	// Aria2DataDir is DataDir as seen by the aria2 process (differs from
	// DataDir only if aria2 runs in another container with another mount).
	Aria2DataDir string

	// ComponentsDir 是被托管组件的安装目录,必须落在可写且持久的卷内,
	// 网页里的升级才不会被容器重启抹掉。目前只有 yt-dlp 装在这里——它
	// 一年发上百个版本,跟着镜像走太慢。aria2 随它自己的容器、ffmpeg
	// 随主镜像,都用 docker compose pull 升级。
	// 留空表示不托管:一律用 PATH 里的版本(本机开发即如此)。
	ComponentsDir string
	// ComponentsBundled 是镜像内置的只读副本目录,首次启动时复制进
	// ComponentsDir。
	ComponentsBundled string
}

func Load() (*Config, error) {
	cfg := &Config{
		// 默认 16688:避开 8080 等常见端口的冲突
		Addr:          envOr("POCKETDRIVE_ADDR", ":16688"),
		DataDir:       envOr("POCKETDRIVE_DATA_DIR", "./data"),
		DBPath:        envOr("POCKETDRIVE_DB", "./pocketdrive.db"),
		AdminUser:     envOr("POCKETDRIVE_ADMIN_USER", "admin"),
		AdminPassword: os.Getenv("POCKETDRIVE_ADMIN_PASSWORD"),
		// 官方 compose 里指向 aria2 容器;本机开发默认连本地 aria2c
		Aria2RPC:    envOr("POCKETDRIVE_ARIA2_RPC", "http://127.0.0.1:6800/jsonrpc"),
		Aria2Secret: os.Getenv("POCKETDRIVE_ARIA2_SECRET"),

		ComponentsDir:     os.Getenv("POCKETDRIVE_BIN_DIR"),
		ComponentsBundled: os.Getenv("POCKETDRIVE_BIN_BUNDLED"),
	}
	cfg.Aria2DataDir = envOr("POCKETDRIVE_ARIA2_DATA_DIR", cfg.DataDir)
	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
