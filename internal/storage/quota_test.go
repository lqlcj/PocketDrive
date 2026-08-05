package storage

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pocketdrive/internal/db"
)

func newTestService(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	g, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db open: %v", err)
	}
	sqlDB, err := g.DB()
	if err != nil {
		t.Fatalf("gorm db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	data := filepath.Join(dir, "data")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	return New(data, os.DirFS(data), g), data
}

func TestSetQuota(t *testing.T) {
	s, _ := newTestService(t)
	if s.Quota() != 0 {
		t.Fatalf("default quota = %d, want 0", s.Quota())
	}
	if err := s.SetQuota(5 << 30); err != nil {
		t.Fatalf("set quota: %v", err)
	}
	if s.Quota() != 5<<30 {
		t.Fatalf("quota = %d, want %d", s.Quota(), int64(5<<30))
	}
	// 负数按"不限"处理
	if err := s.SetQuota(-1); err != nil {
		t.Fatalf("set negative: %v", err)
	}
	if s.Quota() != 0 {
		t.Fatalf("negative quota should clamp to 0, got %d", s.Quota())
	}
}

func TestUsageCountsFiles(t *testing.T) {
	s, data := newTestService(t)
	if err := os.WriteFile(filepath.Join(data, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 回收站里的东西照样占盘,要算
	if err := os.MkdirAll(filepath.Join(data, ".trash"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, ".trash", "deleted"), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 点开头的是内部文件,不算
	if err := os.WriteFile(filepath.Join(data, ".hidden"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.refreshUsage()
	s.uMu.Lock()
	bytes, files := s.usageBytes, s.usageFiles
	s.uMu.Unlock()
	if bytes != 10 || files != 2 {
		t.Fatalf("usage = %d bytes / %d files, want 10 / 2", bytes, files)
	}

	// 有基数后 AddUsage 就地累加
	s.AddUsage(4)
	s.uMu.Lock()
	got := s.usageBytes
	s.uMu.Unlock()
	if got != 14 {
		t.Fatalf("after AddUsage bytes = %d, want 14", got)
	}
}

func TestCheckLocalQuota(t *testing.T) {
	s, data := newTestService(t)
	if err := os.WriteFile(filepath.Join(data, "f.bin"), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	s.uMu.Lock()
	s.usageBytes, s.usageAt = 100, time.Now()
	s.uMu.Unlock()
	if err := s.SetQuota(150); err != nil {
		t.Fatal(err)
	}

	if err := s.CheckLocal(49); err != nil {
		t.Fatalf("100+49 <= 150 should pass, got %v", err)
	}
	err := s.CheckLocal(51)
	if err == nil {
		t.Fatal("100+51 > 150 should fail")
	}
	if _, ok := err.(*FullError); !ok {
		t.Fatalf("want *FullError, got %T", err)
	}

	// 用量未知(没统计过)时放行:软限制
	s2, _ := newTestService(t)
	if err := s2.SetQuota(1); err != nil {
		t.Fatal(err)
	}
	if err := s2.CheckLocal(1 << 20); err != nil {
		t.Fatalf("unknown usage should pass, got %v", err)
	}
}

func TestHandleSetQuota(t *testing.T) {
	s, _ := newTestService(t)
	req := httptest.NewRequest("POST", "/", strings.NewReader(`{"quotaGB":10}`))
	w := httptest.NewRecorder()
	s.HandleSetQuota(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	if s.Quota() != 10<<30 {
		t.Fatalf("quota = %d, want %d", s.Quota(), int64(10<<30))
	}

	// 0 = 不限
	req = httptest.NewRequest("POST", "/", strings.NewReader(`{"quotaGB":0}`))
	w = httptest.NewRecorder()
	s.HandleSetQuota(w, req)
	if w.Code != http.StatusOK || s.Quota() != 0 {
		t.Fatalf("quotaGB=0 should clear quota, got %d (status %d)", s.Quota(), w.Code)
	}
}
