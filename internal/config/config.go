package config

import (
	"os"
)

const Version = "0.5.0"

type Config struct {
	// Addr is the HTTP listen address, e.g. ":8080".
	Addr string
	// DataDir is the root of the drive; all file operations, WebDAV and aria2
	// downloads live under this single directory.
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
