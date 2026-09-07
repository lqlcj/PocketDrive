package dav

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pocketdrive/internal/cloud"
	"pocketdrive/internal/db"
	"pocketdrive/internal/files"
	"pocketdrive/internal/trash"
)

type brokenUpload struct{ sent bool }

func (r *brokenUpload) Read(p []byte) (int, error) {
	if r.sent {
		return 0, errors.New("connection lost")
	}
	r.sent = true
	return copy(p, "partial"), nil
}

func TestPutPreservesOriginalOnIncompleteUpload(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   func() io.Reader
		size   int64
		cancel bool
	}{
		{"read-error", func() io.Reader { return &brokenUpload{} }, -1, false},
		{"short-body", func() io.Reader { return strings.NewReader("short") }, 100, false},
		{"long-body", func() io.Reader { return strings.NewReader("too long") }, 2, false},
		{"cancelled", func() io.Reader { return strings.NewReader("new") }, 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, data := newTestHandler(t)
			if err := os.WriteFile(filepath.Join(data, "keep.txt"), []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodPut, "/dav/keep.txt", tc.body())
			r.ContentLength = tc.size
			if tc.cancel {
				ctx, cancel := context.WithCancel(r.Context())
				cancel()
				r = r.WithContext(ctx)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code < 400 {
				t.Fatalf("unexpected success: %d", w.Code)
			}
			got, err := os.ReadFile(filepath.Join(data, "keep.txt"))
			if err != nil || string(got) != "original" {
				t.Fatalf("original damaged: %q %v", got, err)
			}
			entries, err := os.ReadDir(data)
			if err != nil || len(entries) != 1 {
				t.Fatalf("temporary files leaked: %v %v", entries, err)
			}
		})
	}
}

func TestPutPublishesCompleteUpload(t *testing.T) {
	for _, body := range []string{"", "complete content"} {
		h, data := newTestHandler(t)
		if err := os.WriteFile(filepath.Join(data, "keep.txt"), []byte("old"), 0600); err != nil {
			t.Fatal(err)
		}
		w := davReq(t, h, http.MethodPut, "/dav/keep.txt", body)
		if w.Code != http.StatusCreated {
			t.Fatalf("PUT: %d %s", w.Code, w.Body.String())
		}
		got, err := os.ReadFile(filepath.Join(data, "keep.txt"))
		if err != nil || string(got) != body {
			t.Fatalf("content: %q %v", got, err)
		}
	}
}

func TestMoveAndCopyNeverOverwrite(t *testing.T) {
	for _, method := range []string{"MOVE", "COPY"} {
		for _, overwrite := range []string{"", "T", "F"} {
			h, data := newTestHandler(t)
			for name, content := range map[string]string{"source": "original", "target": "keep"} {
				if err := os.WriteFile(filepath.Join(data, name), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			r := httptest.NewRequest(method, "/dav/source", nil)
			r.Header.Set("Destination", "/dav/target")
			r.Header.Set("Overwrite", overwrite)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != http.StatusPreconditionFailed {
				t.Fatalf("%s %s: %d %s", method, overwrite, w.Code, w.Body.String())
			}
			for name, content := range map[string]string{"source": "original", "target": "keep"} {
				got, err := os.ReadFile(filepath.Join(data, name))
				if err != nil || string(got) != content {
					t.Fatalf("%s: %q %v", name, got, err)
				}
			}
		}
	}
}

func TestDeleteIsRecoverableAndTrashIsProtected(t *testing.T) {
	dir := t.TempDir()
	gdb, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := gdb.DB()
	t.Cleanup(func() { sqlDB.Close() })
	cs := cloud.New(gdb)
	fs, err := files.New(filepath.Join(dir, "data"), filepath.Join(dir, "uploads"), cs, gdb)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Root().Close() })
	ts := trash.New(gdb, fs, cs)
	h := Handler(fs.Root(), cs, ts.Trash)
	if err := fs.Root().Mkdir("folder", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data/folder/keep.txt"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	w := davReq(t, h, "DELETE", "/dav/folder", "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE: %d %s", w.Code, w.Body.String())
	}
	var item db.TrashItem
	if err := gdb.First(&item).Error; err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "DELETE", "PUT", "PROPFIND"} {
		w := davReq(t, h, method, "/dav/.trash/"+item.TrashKey+"/keep.txt", "")
		if w.Code < 400 {
			t.Fatalf("%s can access trash: %d", method, w.Code)
		}
	}
	w = httptest.NewRecorder()
	r := httptest.NewRequest("PROPFIND", "/dav/", nil)
	r.Header.Set("Depth", "1")
	h.ServeHTTP(w, r)
	if strings.Contains(w.Body.String(), ".trash") {
		t.Fatal("trash exposed in listing")
	}
	// A new service uses the persisted record, just as after a restart.
	ts = trash.New(gdb, fs, cs)
	w = httptest.NewRecorder()
	ts.HandleRestore(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"id":1}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", w.Code, w.Body.String())
	}
	got, err := os.ReadFile(filepath.Join(dir, "data/folder/keep.txt"))
	if err != nil || string(got) != "original" {
		t.Fatalf("restored: %q %v", got, err)
	}
}

func TestRemoteDAVDeleteIsDenied(t *testing.T) {
	h, fake, _ := newFakeDav(t, []string{"keep.txt"})
	w := davReq(t, h, "DELETE", "/dav/@"+fakeMount+"/keep.txt", "")
	if w.Code < 400 {
		t.Fatalf("DELETE: %d", w.Code)
	}
	if writes := fake.writes(); len(writes) != 0 {
		t.Fatalf("remote mutations: %v", writes)
	}
}
