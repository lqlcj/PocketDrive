package config

import (
	"fmt"
	"os"
	"strconv"
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

	Aria2RPC    string
	Aria2Secret string
	// Aria2DataDir is DataDir as seen by the aria2 process (differs from
	// DataDir only if aria2 runs in another container with another mount).
	Aria2DataDir string
	// Aria2External 为 true 时不启动本地 aria2c 子进程,只连 Aria2RPC
	// 指向的外部实例(老的 sidecar 部署)。
	Aria2External bool
	Aria2BTPort   int

	// ComponentsDir 是被托管组件(yt-dlp / aria2c / ffmpeg)的安装目录,
	// 必须落在可写且持久的卷内,网页里的升级才不会被容器重启抹掉。
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
		Aria2Secret:   os.Getenv("POCKETDRIVE_ARIA2_SECRET"),
		Aria2BTPort:   envInt("POCKETDRIVE_ARIA2_BT_PORT", 6888),

		ComponentsDir:     os.Getenv("POCKETDRIVE_BIN_DIR"),
		ComponentsBundled: os.Getenv("POCKETDRIVE_BIN_BUNDLED"),
	}
	// 显式配了 RPC 地址 = 连外部 aria2(老的 sidecar 部署),
	// 不配则本地起一个子进程
	if rpc := os.Getenv("POCKETDRIVE_ARIA2_RPC"); rpc != "" {
		cfg.Aria2RPC, cfg.Aria2External = rpc, true
	} else {
		cfg.Aria2RPC = fmt.Sprintf("http://127.0.0.1:%d/jsonrpc",
			envInt("POCKETDRIVE_ARIA2_RPC_PORT", 6800))
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

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}
