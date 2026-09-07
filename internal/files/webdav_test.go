package files

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/net/webdav"
	"pocketdrive/internal/cloud"
)

func TestWebDAVUploadResumeAndDownload(t *testing.T) {
	var failPut atomic.Bool
	dav := &webdav.Handler{FileSystem: webdav.NewMemFS(), LockSystem: webdav.NewMemLS()}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "user" || p != "secret" {
			w.WriteHeader(401)
			return
		}
		if failPut.Load() && r.Method == "PUT" {
			w.WriteHeader(503)
			return
		}
		dav.ServeHTTP(w, r)
	}))
	defer srv.Close()
	svc := localSvc(t)
	config := map[string]any{"name": "VPS", "type": "webdav", "endpoint": srv.URL, "username": "user", "password": "secret"}
	result := mustCall(t, svc.cloud.HandleSave, "POST", "/", config)
	var saved struct {
		ID uint `json:"id"`
	}
	if err := json.Unmarshal(result, &saved); err != nil {
		t.Fatal(err)
	}
	config["id"] = saved.ID
	config["password"] = ""
	mustCall(t, svc.cloud.HandleSave, "POST", "/", config)
	mustCall(t, svc.cloud.HandleTest, "POST", "/", map[string]any{"id": saved.ID})
	listing := mustCall(t, svc.cloud.HandleList, "GET", "/", nil)
	if strings.Contains(string(listing), "secret") || !strings.Contains(string(listing), `"username":"user"`) {
		t.Fatalf("policy response: %s", listing)
	}
	space := &reservationSpace{limit: 100}
	svc.SetLocalSpace(space)
	const target = "@VPS/resume.txt"
	init := doInit(t, svc, target, 6, 100, 3)
	putChunk(t, svc, init.ID, 0, []byte("abc"))
	// Reconstruct the mount service to exercise persistence across reloads.
	svc.cloud = cloud.New(svc.db)
	resume := doInit(t, svc, target, 6, 100, 3)
	if resume.ID != init.ID || len(resume.Uploaded) != 1 || resume.Uploaded[0] != 0 {
		t.Fatalf("resume: %+v", resume)
	}
	putChunk(t, svc, init.ID, 1, []byte("def"))
	failPut.Store(true)
	if code, _ := call(t, svc.HandleUploadComplete, "POST", "/", map[string]any{"id": init.ID, "chunks": 2}); code/100 == 2 {
		t.Fatal("failed upload reported success")
	}
	if _, ok := svc.findSession(init.ID); !ok {
		t.Fatal("failed upload lost resume session")
	}
	failPut.Store(false)
	mustCall(t, svc.HandleUploadComplete, "POST", "/", map[string]any{"id": init.ID, "chunks": 2})
	if len(space.additional) != 0 {
		t.Fatalf("remote upload charged local quota: %v", space.additional)
	}
	if _, err := svc.Root().Stat(target); !os.IsNotExist(err) {
		t.Fatalf("local file: %v", err)
	}
	if list := listOf(t, svc, "@VPS"); len(list.Entries) != 1 || list.Entries[0].Name != "resume.txt" {
		t.Fatalf("list: %+v", list)
	}
	r := httptest.NewRequest("GET", "/?path="+url.QueryEscape(target), nil)
	r.Header.Set("Range", "bytes=2-4")
	w := httptest.NewRecorder()
	svc.HandleDownload(w, r)
	if w.Code != 206 || w.Body.String() != "cde" || w.Header().Get("Location") != "" {
		t.Fatalf("download: %d %s", w.Code, w.Body.String())
	}
	m, _, ok := svc.cloud.Resolve(target)
	if !ok {
		t.Fatal("missing mount")
	}
	if err := m.Put(context.Background(), "active.html", strings.NewReader("<script>x</script>"), 18); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	svc.HandleDownload(w, httptest.NewRequest("GET", "/?path=@VPS/active.html", nil))
	if !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal("active content served inline")
	}
	if err := m.Put(context.Background(), "nested/child/a.txt", strings.NewReader("a"), 1); err != nil {
		t.Fatal(err)
	}
	if err := m.Rename(context.Background(), "nested", "moved"); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(context.Background(), "moved"); err != nil {
		t.Fatal(err)
	}
	// Removing the policy must never make a staged upload land on local disk.
	orphan := doInit(t, svc, "@VPS/orphan.txt", 3, 200, 3)
	putChunk(t, svc, orphan.ID, 0, []byte("abc"))
	mustCall(t, svc.cloud.HandleDelete, "POST", "/", map[string]any{"id": saved.ID})
	if code, _ := call(t, svc.HandleUploadComplete, "POST", "/", map[string]any{"id": orphan.ID, "chunks": 1}); code/100 == 2 {
		t.Fatal("unmounted upload succeeded")
	}
	if _, err := svc.Root().Stat("@VPS/orphan.txt"); !os.IsNotExist(err) {
		t.Fatalf("unmounted upload wrote locally: %v", err)
	}
}
