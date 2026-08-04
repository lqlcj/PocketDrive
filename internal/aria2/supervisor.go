package aria2

// aria2 以子进程方式跑在 PocketDrive 容器里(而不是单独的 sidecar
// 容器),这样它的二进制就和 yt-dlp/ffmpeg 一样能在网页里升级,部署
// 也从两个容器缩成一个。
//
// 仍然支持连外部 aria2:配置了 POCKETDRIVE_ARIA2_RPC 指向别处时不
// 启动本地进程——老的 compose 部署升级上来不会被打断。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"gorm.io/gorm"

	"pocketdrive/internal/db"
)

// Supervisor 负责本地 aria2c 子进程的生命周期。
type Supervisor struct {
	binPath  func() string // 每次重启都重新取:升级后路径不变但文件已换
	dataDir  string
	sessDir  string // 会话/日志目录,放 config 卷里
	rpcPort  int
	btPort   int
	secret   string
	extraLog bool

	mu      sync.Mutex
	cmd     *exec.Cmd
	stopped bool
	// 进程意外退出后自动拉起,但要防止疯狂重启刷屏
	restarts int
	lastAt   time.Time
}

func NewSupervisor(binPath func() string, dataDir, sessDir, secret string, rpcPort, btPort int) *Supervisor {
	return &Supervisor{
		binPath: binPath, dataDir: dataDir, sessDir: sessDir,
		secret: secret, rpcPort: rpcPort, btPort: btPort,
	}
}

// RandomSecret 生成一个 RPC 密钥。本地进程只监听回环,密钥主要是防
// 同机其他进程乱调。
func RandomSecret() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "pocketdrive-local"
	}
	return hex.EncodeToString(b)
}

// LoadOrCreateSecret 取出本地 aria2 的 RPC 密钥,没有就生成一个存库。
// 存库而不是每次随机:重启后仍能连上没被杀掉的 aria2 进程。
func LoadOrCreateSecret(gdb *gorm.DB) string {
	const key = "aria2_local_secret"
	var s db.Setting
	if err := gdb.First(&s, "key = ?", key).Error; err == nil && s.Value != "" {
		return s.Value
	}
	v := RandomSecret()
	gdb.Save(&db.Setting{Key: key, Value: v})
	return v
}

// args 拼 aria2c 启动参数。取值偏向小内存 VPS:不预分配磁盘、
// 磁盘缓存压到 32MB。其余可调项由「下载设置」页通过 RPC 下发,
// 不写在这里,免得两处配置打架。
func (s *Supervisor) args() []string {
	return []string{
		"--enable-rpc",
		"--rpc-listen-all=false", // 只听回环:同容器内访问,不对外暴露
		fmt.Sprintf("--rpc-listen-port=%d", s.rpcPort),
		"--rpc-secret=" + s.secret,
		"--dir=" + s.dataDir,
		"--continue=true",
		"--file-allocation=none", // 小 VPS 上预分配会让大文件卡很久
		"--disk-cache=32M",
		"--max-concurrent-downloads=3",
		"--max-connection-per-server=8",
		"--split=8",
		"--min-split-size=4M",
		"--enable-dht=true",
		fmt.Sprintf("--listen-port=%d", s.btPort),
		fmt.Sprintf("--dht-listen-port=%d", s.btPort),
		"--seed-ratio=0", // 做种策略由设置页控制,这里给个不阻塞的默认
		// 任务列表落盘,重启 aria2 不丢未完成的下载
		"--save-session=" + filepath.Join(s.sessDir, "aria2.session"),
		"--input-file=" + filepath.Join(s.sessDir, "aria2.session"),
		"--save-session-interval=30",
		"--auto-save-interval=30",
		"--console-log-level=warn",
		"--summary-interval=0",
	}
}

// Start 拉起 aria2c 并在它意外退出时自动重启。
func (s *Supervisor) Start() error {
	bin := s.binPath()
	if bin == "" {
		return errors.New("没有可用的 aria2c")
	}
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("aria2c 不可用: %w", err)
	}
	if err := os.MkdirAll(s.sessDir, 0o755); err != nil {
		return err
	}
	// aria2 的 --input-file 指向不存在的文件会直接退出
	sess := filepath.Join(s.sessDir, "aria2.session")
	if _, err := os.Stat(sess); err != nil {
		if f, err := os.Create(sess); err == nil {
			f.Close()
		}
	}
	go s.supervise()
	return nil
}

func (s *Supervisor) supervise() {
	for {
		s.mu.Lock()
		if s.stopped {
			s.mu.Unlock()
			return
		}
		// 连续崩溃时拉长间隔,避免刷爆日志
		if !s.lastAt.IsZero() && time.Since(s.lastAt) < time.Minute {
			s.restarts++
		} else {
			s.restarts = 0
		}
		s.lastAt = time.Now()
		backoff := time.Duration(min(s.restarts, 6)) * 5 * time.Second
		bin := s.binPath()
		cmd := exec.Command(bin, s.args()...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		s.cmd = cmd
		s.mu.Unlock()

		if backoff > 0 {
			time.Sleep(backoff)
		}
		if err := cmd.Start(); err != nil {
			log.Printf("aria2c 启动失败: %v", err)
			time.Sleep(10 * time.Second)
			continue
		}
		err := cmd.Wait()

		s.mu.Lock()
		stopped := s.stopped
		s.mu.Unlock()
		if stopped {
			return
		}
		log.Printf("aria2c 退出(%v),即将重启", err)
	}
}

// Stop 停掉子进程,不再自动重启。
func (s *Supervisor) Stop() {
	s.mu.Lock()
	s.stopped = true
	cmd := s.cmd
	s.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// Restart 重启子进程。升级 aria2c 之后要调一次,新二进制才会生效。
func (s *Supervisor) Restart(ctx context.Context) error {
	s.mu.Lock()
	cmd := s.cmd
	s.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return s.Start()
	}
	// supervise 循环会自己把它拉起来
	if err := cmd.Process.Kill(); err != nil {
		return err
	}
	// 等 RPC 重新可用,免得前端立刻查状态看到「不可达」
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
		s.mu.Lock()
		running := s.cmd != nil && s.cmd.Process != nil && s.cmd.ProcessState == nil
		s.mu.Unlock()
		if running {
			return nil
		}
	}
	return nil
}
