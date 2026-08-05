package logs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 每个用例都要从干净状态开始:包级状态是全局的
func reset(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	mu.Lock()
	if file != nil {
		file.Close()
	}
	file, day, written, dropped = nil, "", 0, false
	mu.Unlock()
	Init(d)
	t.Cleanup(func() {
		mu.Lock()
		if file != nil {
			file.Close()
			file = nil
		}
		dir = ""
		mu.Unlock()
	})
	return d
}

func TestErrorfWritesAndTails(t *testing.T) {
	reset(t)
	Errorf("files", "上传失败: %s", "磁盘满了")
	Errorf("aria2", "任务 abc 失败")

	text, size := Tail(0)
	if size == 0 {
		t.Fatal("文件大小不该是 0")
	}
	if !strings.Contains(text, "[files] 上传失败: 磁盘满了") {
		t.Fatalf("没记上第一条: %q", text)
	}
	if !strings.Contains(text, "[aria2] 任务 abc 失败") {
		t.Fatalf("没记上第二条: %q", text)
	}
}

// 多行错误(yt-dlp 的输出常常是几行)要缩进,不然和下一条混在一起
func TestMultilineIndented(t *testing.T) {
	reset(t)
	Errorf("ytdlp", "失败:\n第一行\n第二行")
	text, _ := Tail(0)
	if !strings.Contains(text, "\n    第一行\n    第二行") {
		t.Fatalf("续行没缩进: %q", text)
	}
}

// 「每天清空」:跨天时旧文件被删掉,磁盘上只留当天这一个
func TestRotateDropsYesterday(t *testing.T) {
	d := reset(t)
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	old := filepath.Join(d, fileName(yesterday))
	if err := os.WriteFile(old, []byte("昨天的\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	Errorf("files", "今天的")

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("跨天后昨天的日志应当被删掉")
	}
	entries, err := os.ReadDir(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != fileName(today()) {
		t.Fatalf("目录里应当只剩今天这一个文件: %v", entries)
	}
	text, _ := Tail(0)
	if strings.Contains(text, "昨天的") {
		t.Fatal("不该读到昨天的内容")
	}
}

func TestTailLimit(t *testing.T) {
	reset(t)
	for i := 0; i < maxTailLines+50; i++ {
		Errorf("x", "第 %d 行", i)
	}
	text, _ := Tail(0)
	if n := strings.Count(text, "\n") + 1; n > maxTailLines {
		t.Fatalf("最多回 %d 行,得到 %d", maxTailLines, n)
	}
	// 要的是最后那些,不是最前面那些
	if !strings.Contains(text, "第 549 行") {
		t.Fatalf("尾部内容不对: %q", text[max(0, len(text)-200):])
	}
}

func TestClear(t *testing.T) {
	reset(t)
	Errorf("x", "一条")
	if err := Clear(); err != nil {
		t.Fatal(err)
	}
	text, size := Tail(0)
	if text != "" || size != 0 {
		t.Fatalf("清空后应当什么都读不到,得到 %q / %d", text, size)
	}
	// 清空之后还能继续写
	Errorf("x", "再来一条")
	text, _ = Tail(0)
	if !strings.Contains(text, "再来一条") {
		t.Fatalf("清空后写不进去了: %q", text)
	}
}

// 没配置目录时(本机 go run)只写 stderr,不该 panic 也不该建文件
func TestDisabled(t *testing.T) {
	mu.Lock()
	if file != nil {
		file.Close()
		file = nil
	}
	dir, day = "", ""
	mu.Unlock()

	Errorf("x", "不落盘")
	if Enabled() {
		t.Fatal("没有目录时 Enabled 应当是 false")
	}
	if Path() != "" {
		t.Fatal("没有目录时 Path 应当是空串")
	}
	if text, size := Tail(0); text != "" || size != 0 {
		t.Fatal("没有目录时 Tail 应当返回空")
	}
	if err := Clear(); err != nil {
		t.Fatalf("没有目录时 Clear 不该报错: %v", err)
	}
}
