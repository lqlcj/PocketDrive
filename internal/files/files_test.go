package files

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type usageTracker struct {
	delta int64
}

func (*usageTracker) CheckLocal(int64) error { return nil }
func (u *usageTracker) AddUsage(delta int64) { u.delta += delta }
func (*usageTracker) UploadLimit() int64     { return 0 }

func newFileTestService(t *testing.T) (*Service, string, *usageTracker) {
	t.Helper()
	root := t.TempDir()
	data := filepath.Join(root, "data")
	tmp := filepath.Join(root, "uploads")
	s, err := New(data, tmp, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Root().Close() })
	usage := &usageTracker{}
	s.SetLocalSpace(usage)
	return s, data, usage
}

func TestCleanPath(t *testing.T) {
	cases := map[string]string{
		"":                "",
		"/":               "",
		"a/b":             "a/b",
		"/a/b/":           "a/b",
		"../..":           "",
		"a/../../etc":     "etc",
		"..\\..\\windows": "windows",
		"a\\b":            "a/b",
		"./a":             "a",
	}
	for in, want := range cases {
		if got := CleanPath(in); got != want {
			t.Errorf("CleanPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidName(t *testing.T) {
	for _, bad := range []string{"", ".", "..", "a/b", `a\b`} {
		if err := validName(bad); err == nil {
			t.Errorf("validName(%q) = nil, want error", bad)
		}
	}
	if err := validName("正常文件.txt"); err != nil {
		t.Errorf("validName(normal) = %v", err)
	}
}

func TestHandleWriteTracksOverwriteDelta(t *testing.T) {
	s, data, usage := newFileTestService(t)
	if err := os.WriteFile(filepath.Join(data, "note.md"), []byte("1234567890"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"path":"note.md","content":"test"}`))
	w := httptest.NewRecorder()
	s.HandleWrite(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if usage.delta != -6 {
		t.Fatalf("overwrite usage delta = %d, want -6", usage.delta)
	}
}

func TestHandleUploadTracksOverwriteDelta(t *testing.T) {
	s, data, usage := newFileTestService(t)
	if err := os.WriteFile(filepath.Join(data, "same.bin"), []byte("1234567890"), 0o644); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "same.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("1234")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/files/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	s.HandleUpload(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	if usage.delta != -6 {
		t.Fatalf("upload overwrite usage delta = %d, want -6", usage.delta)
	}
}
