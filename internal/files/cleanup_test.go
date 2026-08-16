package files

// 自动清理是全服务唯一一个定时删东西的地方,边界必须钉死:只允许它删掉
// 自己建的、名字就是会话 ID 的暂存目录。别的东西——尤其是用户自己建的
// 文件夹——放多久都不能碰,否则就是「文件夹过几天莫名其妙消失」。

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func agedDir(t *testing.T, parent, name string) string {
	t.Helper()
	p := filepath.Join(parent, name)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", name, err)
	}
	old := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatalf("chtimes %s: %v", name, err)
	}
	return p
}

func TestCleanupOnlyTouchesSessionDirs(t *testing.T) {
	svc := localSvc(t)

	// 自己建的孤儿暂存目录:该删
	orphan := agedDir(t, svc.tmpDir, "0123456789abcdef0123456789abcdef")
	// 用户自己建的文件夹(万一 tmpDir 落在了网盘里):一律不许动
	keep := []string{
		agedDir(t, svc.tmpDir, "我的照片"),
		agedDir(t, svc.tmpDir, "movies"),
		agedDir(t, svc.tmpDir, "0123456789abcdef"),                   // 长度不对
		agedDir(t, svc.tmpDir, "0123456789abcdef0123456789abcdefg"),  // 多一位
		agedDir(t, svc.tmpDir, "s30123456789abcdef0123456789abcdef"), // 外部存储会话不落本地盘
		agedDir(t, svc.tmpDir, "AABBCCDDEEFF00112233445566778899"),   // 大写不是我们生成的
		agedDir(t, svc.tmpDir, filepath.Join("我的照片", "去年")),          // 嵌套的也不该被波及
	}
	keepFile := filepath.Join(svc.tmpDir, "0123456789abcdef0123456789abcdee")
	if err := os.WriteFile(keepFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	old := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(keepFile, old, old); err != nil {
		t.Fatalf("chtimes file: %v", err)
	}

	svc.cleanupOnce()

	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("孤儿暂存目录应当被清掉,stat err=%v", err)
	}
	for _, p := range keep {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("不该被清理的目录被删了: %s (%v)", p, err)
		}
	}
	if _, err := os.Stat(keepFile); err != nil {
		t.Errorf("不该被清理的文件被删了: %v", err)
	}
}

func TestCleanupKeepsFreshSessionDir(t *testing.T) {
	svc := localSvc(t)
	fresh := filepath.Join(svc.tmpDir, "abcdef0123456789abcdef0123456789")
	if err := os.MkdirAll(fresh, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	svc.cleanupOnce()

	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("未超时的暂存目录不该被清掉: %v", err)
	}
}
