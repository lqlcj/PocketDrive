package files

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type usageTracker struct {
	delta            int64
	checkErr         error
	additionalChecks []int64
	temporaryChecks  []int64
}

func (u *usageTracker) CheckLocal(int64) error { return u.checkErr }
func (u *usageTracker) CheckLocalSpace(additional, temporary int64) error {
	u.additionalChecks = append(u.additionalChecks, additional)
	u.temporaryChecks = append(u.temporaryChecks, temporary)
	return u.checkErr
}
func (u *usageTracker) CheckPathSpace(string, int64) error { return u.checkErr }
func (u *usageTracker) AddUsage(delta int64)               { u.delta += delta }
func (*usageTracker) UploadLimit() int64                   { return 0 }

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

type failingReader struct {
	done bool
}

func (r *failingReader) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true
		copy(p, "new partial")
		return len("new partial"), nil
	}
	return 0, errors.New("simulated read failure")
}

func TestAtomicWriteKeepsOldFileOnFailure(t *testing.T) {
	s, data, usage := newFileTestService(t)
	const original = "original content"
	if err := os.WriteFile(filepath.Join(data, "same.bin"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.atomicWrite("same.bin", &failingReader{}, -1, ""); err == nil {
		t.Fatal("write should fail")
	}
	got, err := os.ReadFile(filepath.Join(data, "same.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("old file was damaged: %q", got)
	}
	if usage.delta != 0 {
		t.Fatalf("failed write changed usage by %d", usage.delta)
	}
	entries, err := os.ReadDir(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".pd-write-") {
			t.Fatalf("temporary file leaked: %s", e.Name())
		}
	}
}

func TestAtomicWriteValidatesExpectedSizeBeforeReplace(t *testing.T) {
	s, data, _ := newFileTestService(t)
	if err := os.WriteFile(filepath.Join(data, "same.bin"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.atomicWrite("same.bin", io.NopCloser(strings.NewReader("short")), 20, ""); err == nil {
		t.Fatal("size mismatch should fail")
	}
	got, _ := os.ReadFile(filepath.Join(data, "same.bin"))
	if string(got) != "old" {
		t.Fatalf("size mismatch replaced old file: %q", got)
	}
}

func TestAtomicWriteChecksCumulativeTemporarySize(t *testing.T) {
	s, _, usage := newFileTestService(t)
	content := bytes.Repeat([]byte("x"), (1<<20)+17)
	if _, _, err := s.atomicWrite("large.bin", bytes.NewReader(content), -1, ""); err != nil {
		t.Fatal(err)
	}
	if len(usage.temporaryChecks) < 2 {
		t.Fatalf("temporary checks = %v, want one check per read block", usage.temporaryChecks)
	}
	if got := usage.temporaryChecks[len(usage.temporaryChecks)-1]; got != int64(len(content)) {
		t.Fatalf("last temporary check = %d, want cumulative %d", got, len(content))
	}
}

func TestDangerousDownloadIsForcedAttachment(t *testing.T) {
	s, data, _ := newFileTestService(t)
	if err := os.WriteFile(filepath.Join(data, "attack.html"), []byte("<script>alert(1)</script>"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.HandleDownload(w, httptest.NewRequest(http.MethodGet, "/?path=attack.html", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("disposition = %q", w.Header().Get("Content-Disposition"))
	}
	if w.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("content-type = %q", w.Header().Get("Content-Type"))
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("CSP = %q", w.Header().Get("Content-Security-Policy"))
	}
}

func TestPdfDownloadRemainsInlineCapable(t *testing.T) {
	s, data, _ := newFileTestService(t)
	if err := os.WriteFile(filepath.Join(data, "safe.pdf"), []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.HandleDownload(w, httptest.NewRequest(http.MethodGet, "/?path=safe.pdf", nil))
	if got := w.Header().Get("Content-Disposition"); got != "" {
		t.Fatalf("PDF was forced to download: %q", got)
	}
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

func TestHandleUploadDoesNotChargeMultipartFramingAsFileUsage(t *testing.T) {
	s, _, usage := newFileTestService(t)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "tiny.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("x")); err != nil {
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
	for _, checked := range usage.temporaryChecks {
		if checked > 1 {
			t.Fatalf("temporary space check used multipart request size %d instead of file bytes", checked)
		}
	}
}

func TestHandleUploadAllowsOverwriteWithinFinalQuota(t *testing.T) {
	s, data, usage := newFileTestService(t)
	if err := os.WriteFile(filepath.Join(data, "same.bin"), bytes.Repeat([]byte("o"), 1024), 0o644); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "same.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(bytes.Repeat([]byte("n"), 1024)); err != nil {
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
	if len(usage.additionalChecks) == 0 {
		t.Fatal("upload did not perform a final quota check")
	}
	for _, checked := range usage.additionalChecks {
		if checked != 0 {
			t.Fatalf("final quota check = %d, want 0 for same-sized overwrite", checked)
		}
	}
	for _, checked := range usage.temporaryChecks {
		if checked > 1024 {
			t.Fatalf("temporary check = %d, want file size only", checked)
		}
	}
}
