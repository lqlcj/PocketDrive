package watch

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// logCapture 把标准库 log 的输出接到 buffer 上,方便断言到底报没报。
func logCapture(t *testing.T) *strings.Builder {
	t.Helper()
	var buf strings.Builder
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })
	return &buf
}

func svc(t *testing.T) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	state := filepath.Join(t.TempDir(), "sentinel.json")
	return New(os.DirFS(root), state), root
}

func write(t *testing.T, root, rel string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestSentinelReportsDisappearance(t *testing.T) {
	s, root := svc(t)
	write(t, root, "电影/片子.mkv")
	if err := os.MkdirAll(filepath.Join(root, "空文件夹"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	buf := logCapture(t)
	s.check() // 建立基准
	if !strings.Contains(buf.String(), "基准快照") {
		t.Fatalf("第一轮应当只记基准线,得到: %s", buf.String())
	}

	// 模拟「外面」把东西删了
	if err := os.RemoveAll(filepath.Join(root, "空文件夹")); err != nil {
		t.Fatalf("rm: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(root, "电影")); err != nil {
		t.Fatalf("rm: %v", err)
	}

	buf.Reset()
	s.check()
	out := buf.String()
	if !strings.Contains(out, "[消失]") {
		t.Fatalf("消失了却没报: %s", out)
	}
	if !strings.Contains(out, "空文件夹") || !strings.Contains(out, "电影") {
		t.Errorf("应当点名消失的条目: %s", out)
	}
	// 父目录整个没了,子文件不该单独再报一行
	if strings.Contains(out, "片子.mkv") {
		t.Errorf("子路径不该重复报: %s", out)
	}
}

func TestSentinelQuietWhenNothingLost(t *testing.T) {
	s, root := svc(t)
	write(t, root, "照片/a.jpg")

	logCapture(t)
	s.check()

	write(t, root, "照片/b.jpg") // 只增不减
	buf := logCapture(t)
	s.check()
	if strings.Contains(buf.String(), "[消失]") {
		t.Fatalf("只新增不该报消失: %s", buf.String())
	}
}

func TestSentinelIgnoresHidden(t *testing.T) {
	s, root := svc(t)
	write(t, root, ".trash/abc/旧文件.txt")
	write(t, root, "留着.txt")

	logCapture(t)
	s.check()
	if err := os.RemoveAll(filepath.Join(root, ".trash")); err != nil {
		t.Fatalf("rm: %v", err)
	}

	buf := logCapture(t)
	s.check()
	if strings.Contains(buf.String(), "[消失]") {
		t.Fatalf("回收站到期清理不该报警: %s", buf.String())
	}
}
