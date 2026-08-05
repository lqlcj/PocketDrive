package dav

// WebDAV handler 的直连包装:确认它只在"真的是外部存储里的文件"时
// 才 302,其余一律照旧交给 webdav.Handler 中转。
//
// 真实存储桶上的直连(302 → 桶,跟过去能拿到内容)由 E2E 测试覆盖,
// 需要 PD_S3_* 凭据,见 dav_e2e_test.go。

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"pocketdrive/internal/cloud"
	"pocketdrive/internal/db"
)

// newTestHandler 起一个没有任何挂载的 WebDAV(直连开关默认开)。
func newTestHandler(t *testing.T) (http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	gdb, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	// Windows 下不关连接的话 TempDir 删不掉 sqlite 文件
	t.Cleanup(func() {
		if sqlDB, err := gdb.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return Handler(dataDir, cloud.New(gdb)), dataDir
}

func get(t *testing.T, h http.Handler, p string) *http.Response {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, p, nil))
	return w.Result()
}

// 本机文件不受直连逻辑影响:照常由 VPS 提供内容。
func TestLocalFileStillServed(t *testing.T) {
	h, dataDir := newTestHandler(t)
	if err := os.WriteFile(filepath.Join(dataDir, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	resp := get(t, h, "/dav/hello.txt")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if string(b) != "hi" {
		t.Errorf("内容 = %q, want %q", b, "hi")
	}
}

// 挂载不存在时不能瞎签名:应当落回 webdav 的 404,而不是把客户端
// 甩到某个桶上。
func TestUnknownMountNotRedirected(t *testing.T) {
	h, _ := newTestHandler(t)
	for _, p := range []string{"/dav/@Nope/song.flac", "/dav/@Nope"} {
		resp := get(t, h, p)
		resp.Body.Close()
		if resp.StatusCode == http.StatusFound {
			t.Errorf("GET %s 被重定向到 %q,期望不重定向", p, resp.Header.Get("Location"))
		}
	}
}

// 根目录列表(PROPFIND)不该被直连逻辑碰到。
func TestPropfindNotRedirected(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PROPFIND", "/dav/", nil)
	req.Header.Set("Depth", "1")
	h.ServeHTTP(w, req)
	if w.Code != http.StatusMultiStatus {
		t.Errorf("PROPFIND 状态码 = %d, want 207", w.Code)
	}
}
