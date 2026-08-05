// Package logs 是一个只记错误的日志文件,给「出问题了但现场已经过去」
// 的情况留证据。
//
// 三条刻意的取舍:
//   - **只记 error**。正常的访问日志一天几万行,既没人看又会把有用的
//     东西淹掉,还得操心磁盘占用。
//   - **每天清空**。文件名带日期,写第一条时发现日期变了就把旧的删掉。
//     所以任何时候磁盘上最多只有当天一个文件,不需要额外的清理任务。
//   - **同时写一份到 stderr**,`docker logs` 照样看得到;网页里的这份
//     只是为了不用 ssh 上服务器。
//
// 没配置目录(本机开发直接 go run)时整个包退化成只写 stderr,不落盘。
package logs

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	// 单个文件的上限。真出了循环报错也不能把小 VPS 的盘写爆:
	// 到顶之后停止写入,只在 stderr 继续。
	maxSize = 4 << 20
	// 网页一次最多回多少行
	maxTailLines = 500
)

var (
	mu      sync.Mutex
	dir     string
	day     string // 当前文件对应的日期,用来判断是否跨天
	file    *os.File
	written int64
	dropped bool // 因为超出上限而停止写入
)

// Init 指定日志目录(通常是配置目录下的 logs/)。传空串 = 不落盘。
func Init(d string) {
	mu.Lock()
	defer mu.Unlock()
	dir = d
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("日志目录创建失败,只写 stderr: %v", err)
		dir = ""
	}
}

func today() string { return time.Now().Format("2006-01-02") }

func fileName(d string) string { return "error-" + d + ".log" }

// rotate 确保 file 指向今天的文件。跨天时关掉旧的、删掉目录里所有不是
// 今天的日志——这就是「每天清空」。调用方必须持有 mu。
func rotate() error {
	if dir == "" {
		return os.ErrNotExist
	}
	d := today()
	if file != nil && day == d {
		return nil
	}
	if file != nil {
		file.Close()
		file = nil
	}
	day, written, dropped = d, 0, false

	// 清掉往日的
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			n := e.Name()
			if strings.HasPrefix(n, "error-") && strings.HasSuffix(n, ".log") &&
				n != fileName(d) {
				_ = os.Remove(filepath.Join(dir, n))
			}
		}
	}

	f, err := os.OpenFile(filepath.Join(dir, fileName(d)),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	if fi, err := f.Stat(); err == nil {
		written = fi.Size()
	}
	file = f
	return nil
}

// Errorf 记一条错误。component 是出问题的模块名(files / aria2 等),
// 出现在每行开头,方便过滤。
func Errorf(component, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	// stderr 那份始终要有:容器日志和这里的文件互为备份
	log.Printf("[%s] %s", component, msg)

	mu.Lock()
	defer mu.Unlock()
	if dir == "" {
		return
	}
	if err := rotate(); err != nil {
		return
	}
	if written >= maxSize {
		if !dropped {
			dropped = true
			// 只提示一次,不然这行自己就成了刷屏的那个
			_, _ = file.WriteString(time.Now().Format("15:04:05") +
				" [logs] 今日日志已达上限,后续错误只写容器日志\n")
		}
		return
	}
	// 多行错误输出缩进续行,避免和下一条混在一起
	line := time.Now().Format("15:04:05") + " [" + component + "] " +
		strings.ReplaceAll(strings.TrimRight(msg, "\n"), "\n", "\n    ") + "\n"
	n, err := file.WriteString(line)
	if err == nil {
		written += int64(n)
	}
}

// Tail 返回最近 n 行(最多 maxTailLines),以及当前文件的大小。
func Tail(n int) (string, int64) {
	mu.Lock()
	defer mu.Unlock()
	if dir == "" {
		return "", 0
	}
	if n <= 0 || n > maxTailLines {
		n = maxTailLines
	}
	b, err := os.ReadFile(filepath.Join(dir, fileName(today())))
	if err != nil {
		return "", 0
	}
	size := int64(len(b))
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return "", size
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n"), size
}

// Clear 立刻清空今天的日志。
func Clear() error {
	mu.Lock()
	defer mu.Unlock()
	if dir == "" {
		return nil
	}
	if file != nil {
		file.Close()
		file = nil
	}
	day, written, dropped = "", 0, false
	return os.Remove(filepath.Join(dir, fileName(today())))
}

// Path 返回今天的日志文件路径(供下载)。没配置目录时返回空串。
func Path() string {
	mu.Lock()
	defer mu.Unlock()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, fileName(today()))
}

// Enabled 报告是否真的在落盘。
func Enabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return dir != ""
}
