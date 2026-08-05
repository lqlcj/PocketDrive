package aria2

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pocketdrive/internal/db"
)

// mockAria2 is a minimal aria2 JSON-RPC server for regression tests.
type mockAria2 struct {
	t        *testing.T
	statuses map[string]*Status // gid -> status
	added    []string           // uris received by addUri
	dirs     []string
	nextGID  int
}

func (m *mockAria2) handler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.t.Fatalf("bad rpc request: %v", err)
	}
	write := func(result any) {
		b, _ := json.Marshal(result)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": "pocketdrive", "result": json.RawMessage(b),
		})
	}
	// params[0] is the secret token ("token:s3cret")
	var token string
	_ = json.Unmarshal(req.Params[0], &token)
	if token != "token:s3cret" {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0", "id": "pocketdrive",
			"error": map[string]any{"code": 1, "message": "Unauthorized"},
		})
		return
	}
	switch req.Method {
	case "aria2.getVersion":
		write(map[string]string{"version": "1.37.0-mock"})
	case "aria2.addUri":
		var uris []string
		var opts map[string]string
		_ = json.Unmarshal(req.Params[1], &uris)
		_ = json.Unmarshal(req.Params[2], &opts)
		m.added = append(m.added, uris[0])
		m.dirs = append(m.dirs, opts["dir"])
		m.nextGID++
		gid := "gid" + string(rune('0'+m.nextGID))
		m.statuses[gid] = &Status{GID: gid, Status: "active"}
		write(gid)
	case "aria2.tellStatus":
		var gid string
		_ = json.Unmarshal(req.Params[1], &gid)
		st, ok := m.statuses[gid]
		if !ok {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": "pocketdrive",
				"error": map[string]any{"code": 1, "message": "GID " + gid + " is not found"},
			})
			return
		}
		write(st)
	case "aria2.pause", "aria2.unpause", "aria2.remove", "aria2.removeDownloadResult":
		write("ok")
	default:
		m.t.Fatalf("unexpected method %s", req.Method)
	}
}

func newTestManager(t *testing.T) (*Manager, *mockAria2) {
	t.Helper()
	mock := &mockAria2{t: t, statuses: map[string]*Status{}}
	srv := httptest.NewServer(http.HandlerFunc(mock.handler))
	t.Cleanup(srv.Close)

	gdb, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	// Windows: TempDir 清理前必须关掉 SQLite 连接,否则文件被占用
	t.Cleanup(func() {
		if sqlDB, err := gdb.DB(); err == nil {
			sqlDB.Close()
		}
	})
	c := NewClient(srv.URL, "s3cret")
	// localDir 指向临时目录:ensureDir 会真的建文件夹,别写到别处去
	return NewManager(gdb, c, "/data", t.TempDir()), mock
}

func TestAddAndSync(t *testing.T) {
	m, mock := newTestManager(t)

	task, err := m.Add("https://example.com/file.iso", "isos")
	if err != nil {
		t.Fatal(err)
	}
	if len(mock.dirs) != 1 || mock.dirs[0] != "/data/isos" {
		t.Fatalf("dir passed to aria2 = %v, want /data/isos", mock.dirs)
	}

	mock.statuses[task.GID] = &Status{
		GID: task.GID, Status: "active",
		TotalLength: "1000", CompletedLength: "500", DownloadSpeed: "42",
		Files: []struct {
			Path string `json:"path"`
		}{{Path: "/data/isos/file.iso"}},
	}
	m.sync()

	_, tasks := m.List()
	if len(tasks) != 1 {
		t.Fatalf("want 1 task, got %d", len(tasks))
	}
	got := tasks[0]
	if got.CompletedLength != 500 || got.TotalLength != 1000 || got.Speed != 42 {
		t.Fatalf("progress not synced: %+v", got)
	}
	if got.Name != "file.iso" {
		t.Fatalf("name = %q, want file.iso", got.Name)
	}
}

func TestMagnetFollowedByMigration(t *testing.T) {
	m, mock := newTestManager(t)

	task, err := m.Add("magnet:?xt=urn:btih:abcdef", "bt")
	if err != nil {
		t.Fatal(err)
	}
	metaGID := task.GID

	// 元数据下载完成,真实下载迁移到新 gid
	mock.statuses["realgid"] = &Status{
		GID: "realgid", Status: "active",
		TotalLength: "2000", CompletedLength: "100",
		Bittorrent: &struct {
			Info *struct {
				Name string `json:"name"`
			} `json:"info"`
		}{Info: &struct {
			Name string `json:"name"`
		}{Name: "ubuntu.iso"}},
	}
	mock.statuses[metaGID] = &Status{
		GID: metaGID, Status: "complete", FollowedBy: []string{"realgid"},
	}
	m.sync()

	_, tasks := m.List()
	if len(tasks) != 1 {
		t.Fatalf("want 1 task after migration, got %d", len(tasks))
	}
	if tasks[0].GID != "realgid" {
		t.Fatalf("gid = %q, want realgid", tasks[0].GID)
	}
	if tasks[0].Name != "ubuntu.iso" {
		t.Fatalf("name = %q, want ubuntu.iso", tasks[0].Name)
	}
	if tasks[0].Dir != "bt" {
		t.Fatalf("dir lost in migration: %q", tasks[0].Dir)
	}
}

func TestGIDLostAfterAria2Restart(t *testing.T) {
	m, mock := newTestManager(t)
	task, _ := m.Add("https://example.com/x.bin", "")
	delete(mock.statuses, task.GID) // aria2 重启,gid 消失
	m.sync()
	_, tasks := m.List()
	if tasks[0].Status != "error" {
		t.Fatalf("status = %q, want error", tasks[0].Status)
	}
}

func TestValidURL(t *testing.T) {
	for _, ok := range []string{"https://a.com/f", "http://a.com/f", "ftp://a.com/f", "magnet:?xt=urn:btih:x"} {
		if err := validURL(ok); err != nil {
			t.Errorf("validURL(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"file:///etc/passwd", "javascript:alert(1)", "notaurl", ""} {
		if err := validURL(bad); err == nil {
			t.Errorf("validURL(%q) = nil, want error", bad)
		}
	}
}

// aria2 报权限错时给的是两句没头没尾的英文,尤其 "Download aborted."
// 完全看不出跟权限有关(它只是 initAndOpenFile 失败的外层文案)。
func TestFriendlyErr(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"Timeout.", "Timeout."},
		{
			"Failed to make the directory /data/x, cause: Permission denied",
			"PUID=0",
		},
		{"Download aborted.", "PUID=0"},
	}
	for _, c := range cases {
		got := friendlyErr(c.in)
		if c.want == "" || c.want == c.in {
			if got != c.in {
				t.Fatalf("%q 不该被改写,得到 %q", c.in, got)
			}
			continue
		}
		if !strings.Contains(got, c.want) {
			t.Fatalf("%q 应当带上修复提示,得到 %q", c.in, got)
		}
		if !strings.HasPrefix(got, c.in) {
			t.Fatalf("原始报错要保留在前面,得到 %q", got)
		}
	}
}

// 默认保存目录是网盘根目录:早期版本默认 "downloads",用户不想要
func TestDefaultDirIsRoot(t *testing.T) {
	if d := defaultSettings().DefaultDir; d != "" {
		t.Fatalf("默认下载目录应当是根目录,得到 %q", d)
	}
}

// 目标目录由 PocketDrive 预先建好(aria2 容器万一没权限,起码目录是在的),
// 但 add 失败时不能在用户网盘里留下一个空文件夹。
func TestEnsureDirOnlyOnSuccess(t *testing.T) {
	m, _ := newTestManager(t)

	if _, err := m.Add("https://example.com/a.iso", "会建出来的"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.localDir, "会建出来的")); err != nil {
		t.Fatalf("成功时应当把目录建出来: %v", err)
	}

	// 换成一个必然连不上的 aria2
	m.c = NewClient("http://127.0.0.1:1/jsonrpc", "s3cret")
	if _, err := m.Add("https://example.com/b.iso", "不该出现的"); err == nil {
		t.Fatal("aria2 不可达时应当报错")
	}
	if _, err := os.Stat(filepath.Join(m.localDir, "不该出现的")); !os.IsNotExist(err) {
		t.Fatal("add 失败不该在网盘里留下空文件夹")
	}
}
