package cloud

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/webdav"
	"pocketdrive/internal/db"
)

func TestWebDAVMount(t *testing.T) {
	ctx := context.Background()
	fs := webdav.NewMemFS()
	if err := fs.Mkdir(ctx, "/base", 0755); err != nil {
		t.Fatal(err)
	}
	h := &webdav.Handler{Prefix: "/dav", FileSystem: fs, LockSystem: webdav.NewMemLS()}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "user" || p != " secret " {
			w.WriteHeader(401)
			return
		}
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()
	m, err := newWebDAVMount(&db.StoragePolicy{Name: "Remote", Endpoint: srv.URL + "/dav/", BasePath: "base", Username: "user", Password: " secret "})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Mkdir(ctx, "folder"); err != nil {
		t.Fatal(err)
	}
	const file = "folder/space # % + &.txt"
	if err := m.Put(ctx, file, strings.NewReader("abcdef"), 6); err != nil {
		t.Fatal(err)
	}
	entries, err := m.List(ctx, "folder")
	if err != nil || len(entries) != 1 || entries[0].Name != "space # % + &.txt" || entries[0].Size != 6 {
		t.Fatalf("list: %+v %v", entries, err)
	}
	f, _, err := m.Open(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(2, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(f)
	if err != nil || string(b) != "cdef" {
		t.Fatalf("range: %q %v", b, err)
	}
	f.Close()
	var count int
	if err := m.WalkFiles(ctx, "", func(p string, n int64, _ time.Time) error {
		count++
		if p != file || n != 6 {
			t.Errorf("walk: %s %d", p, n)
		}
		return nil
	}); err != nil || count != 1 {
		t.Fatalf("walk: %d %v", count, err)
	}
	if err := m.Rename(ctx, "folder", "renamed"); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(ctx, "renamed"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Stat(ctx, "renamed"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	for _, p := range []string{"../outside", "folder/../../outside", "a\\b"} {
		if err := m.Put(ctx, p, strings.NewReader("x"), 1); err == nil {
			t.Fatalf("accepted path %q", p)
		}
	}
	if err := m.Delete(ctx, ""); err == nil {
		t.Fatal("deleted mount root")
	}
	m.password = "wrong"
	if err := m.Ping(ctx); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("authentication: %v", err)
	}
}

func TestWebDAVRootAndProxy(t *testing.T) {
	ctx := context.Background()
	fs := webdav.NewMemFS()
	upstream := httptest.NewServer(&webdav.Handler{FileSystem: fs, LockSystem: webdav.NewMemLS()})
	defer upstream.Close()
	m, err := newWebDAVMount(&db.StoragePolicy{Name: "Remote", Endpoint: upstream.URL})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Put(ctx, "a.txt", strings.NewReader("content"), -1); err != nil {
		t.Fatal(err)
	}
	list, err := m.List(ctx, "")
	if err != nil || len(list) != 1 {
		t.Fatalf("root: %+v %v", list, err)
	}
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	dav := NewDavFSRoot(&Service{mounts: map[string]Mount{"Remote": m}}, root)
	h := &webdav.Handler{FileSystem: dav, LockSystem: webdav.NewMemLS()}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/@Remote/a.txt", nil)
	r.Header.Set("Range", "bytes=1-3")
	h.ServeHTTP(w, r)
	if w.Code != 206 || w.Body.String() != "ont" {
		t.Fatalf("proxy: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("PUT", "/@Remote/b.txt", strings.NewReader("new")))
	if w.Code/100 != 2 {
		t.Fatalf("proxy write: %d %s", w.Code, w.Body.String())
	}
	f, _, err := m.Open(ctx, "b.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil || string(b) != "new" {
		t.Fatalf("proxy body: %q %v", b, err)
	}
}

func TestWebDAVRedirectAndRangeFallback(t *testing.T) {
	var leaked bool
	outside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked = true; w.WriteHeader(200) }))
	defer outside.Close()
	h := &webdav.Handler{FileSystem: webdav.NewMemFS(), LockSystem: webdav.NewMemLS()}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, outside.URL, 307)
			return
		}
		if r.Method == "GET" {
			r.Header.Del("Range")
		}
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()
	m, err := newWebDAVMount(&db.StoragePolicy{Endpoint: srv.URL, Username: "user", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Stat(context.Background(), "redirect"); err == nil {
		t.Fatal("external redirect accepted")
	}
	if leaked {
		t.Fatal("credentials sent outside mount")
	}
	if err := m.Put(context.Background(), "file", strings.NewReader("abcdef"), 6); err != nil {
		t.Fatal(err)
	}
	f, _, err := m.Open(context.Background(), "file")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Seek(3, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(f)
	if err != nil || string(b) != "def" {
		t.Fatalf("range fallback: %q %v", b, err)
	}
}
