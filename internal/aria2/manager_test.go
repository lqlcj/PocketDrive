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
	t              *testing.T
	statuses       map[string]*Status // gid -> status
	added          []string           // uris received by addUri
	dirs           []string
	torrentPaused  []string // pause option received by addTorrent
	lastSelect     string   // select-file passed to changeOption
	unpaused       []string // gids passed to aria2.unpause
	nextGID        int
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
	nextGID := func() string {
		m.nextGID++
		return "gid" + string(rune('0'+m.nextGID))
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
		gid := nextGID()
		m.statuses[gid] = &Status{GID: gid, Status: "active"}
		write(gid)
	case "aria2.addTorrent":
		var opts map[string]string
		_ = json.Unmarshal(req.Params[3], &opts)
		m.torrentPaused = append(m.torrentPaused, opts["pause"])
		gid := nextGID()
		m.statuses[gid] = &Status{GID: gid, Status: "paused"}
		write(gid)
	case "aria2.changeOption":
		var gid string
		var opts map[string]string
		_ = json.Unmarshal(req.Params[1], &gid)
		_ = json.Unmarshal(req.Params[2], &opts)
		m.lastSelect = opts["select-file"]
		write("ok")
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
	case "aria2.pause", "aria2.remove", "aria2.removeDownloadResult":
		write("ok")
	case "aria2.unpause":
		var gid string
		_ = json.Unmarshal(req.Params[1], &gid)
		m.unpaused = append(m.unpaused, gid)
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
		Files: []File{{Path: "/data/isos/file.iso"}},
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

// 上传的 .torrent 走「选完再下」流程:paused 时 addTorrent 必须带上
// pause=true,任务落库状态也是 paused;不要求暂停时则照常立即开始。
func TestAddTorrentPaused(t *testing.T) {
	m, mock := newTestManager(t)

	if _, err := m.AddTorrent("ZA==", "bt", "x.torrent", true); err != nil {
		t.Fatal(err)
	}
	if len(mock.torrentPaused) != 1 || mock.torrentPaused[0] != "true" {
		t.Fatalf("pause=true 没有下发给 aria2: %v", mock.torrentPaused)
	}
	_, tasks := m.List()
	if tasks[0].Status != "paused" {
		t.Fatalf("任务落库状态 = %q, want paused", tasks[0].Status)
	}

	if _, err := m.AddTorrent("ZA==", "bt", "y.torrent", false); err != nil {
		t.Fatal(err)
	}
	if mock.torrentPaused[1] != "" {
		t.Fatalf("不暂停时不该带 pause 选项: %q", mock.torrentPaused[1])
	}
}

// 磁力链元数据就绪后,前端先暂停再勾选:select-file 下发正确、任务恢复。
func TestSelectFiles(t *testing.T) {
	m, mock := newTestManager(t)
	task, err := m.AddTorrent("ZA==", "bt", "x.torrent", true)
	if err != nil {
		t.Fatal(err)
	}
	mock.statuses[task.GID] = &Status{GID: task.GID, Status: "paused"}

	if err := m.SelectFiles(task.GID, []int{1, 3}); err != nil {
		t.Fatal(err)
	}
	if mock.lastSelect != "1,3" {
		t.Fatalf("select-file = %q, want 1,3", mock.lastSelect)
	}
	if len(mock.unpaused) != 1 || mock.unpaused[0] != task.GID {
		t.Fatalf("应当 unpause %q, 得到 %v", task.GID, mock.unpaused)
	}

	// 不传文件序号 = 下载全部:不发 select-file,直接恢复
	if err := m.SelectFiles(task.GID, nil); err != nil {
		t.Fatal(err)
	}
	if len(mock.unpaused) != 2 {
		t.Fatalf("第二次也要 unpause, 得到 %v", mock.unpaused)
	}
}

// 文件清单按 aria2 的顺序编号(1-based),种子名取 bittorrent.info.name。
func TestTorrentFiles(t *testing.T) {
	m, mock := newTestManager(t)
	mock.statuses["gid1"] = &Status{
		GID: "gid1", Status: "paused",
		Bittorrent: &struct {
			Info *struct {
				Name string `json:"name"`
			} `json:"info"`
		}{Info: &struct {
			Name string `json:"name"`
		}{Name: "mytorrent"}},
		Files: []File{
			{Path: "/data/bt/mytorrent/a.mkv", Length: "100"},
			{Path: "/data/bt/mytorrent/b.mp4", Length: "200"},
		},
	}

	name, files, err := m.TorrentFiles("gid1")
	if err != nil {
		t.Fatal(err)
	}
	if name != "mytorrent" {
		t.Fatalf("name = %q, want mytorrent", name)
	}
	if len(files) != 2 {
		t.Fatalf("want 2 files, got %d", len(files))
	}
	if files[0].Index != 1 || files[0].Length != 100 {
		t.Fatalf("files[0] = %+v, want index 1 / length 100", files[0])
	}
	if files[1].Index != 2 || files[1].Length != 200 {
		t.Fatalf("files[1] = %+v, want index 2 / length 200", files[1])
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
	if tasks[0].Follows != metaGID {
		t.Fatalf("migrated task 应当记下旧 gid(follows),得到 %q", tasks[0].Follows)
	}
}

// 磁力元数据就绪后,前端仍拿着旧 gid 轮询文件清单:resolveGID 必须把它
// 翻到 followedBy 的真实 gid,否则永远只看到 [METADATA] 伪文件、弹框
// 一直转圈(「上传的种子能加载」正是因为它没有 follow 这一步)。
func TestMagnetTorrentFilesViaOldGID(t *testing.T) {
	m, mock := newTestManager(t)

	task, err := m.Add("magnet:?xt=urn:btih:abcdef", "bt")
	if err != nil {
		t.Fatal(err)
	}
	metaGID := task.GID

	mock.statuses["realgid"] = &Status{
		GID: "realgid", Status: "active",
		Bittorrent: &struct {
			Info *struct {
				Name string `json:"name"`
			} `json:"info"`
		}{Info: &struct {
			Name string `json:"name"`
		}{Name: "ubuntu.iso"}},
		Files: []File{{Path: "/data/bt/ubuntu.iso/iso.mkv", Length: "100"}},
	}
	mock.statuses[metaGID] = &Status{
		GID: metaGID, Status: "complete", FollowedBy: []string{"realgid"},
		Files: []File{{Path: "/data/bt/[METADATA]magnet:?xt=urn:btih:abcdef", Length: "0"}},
	}

	// aria2 已 follow、DB 还没迁移时,拿旧 gid 要能拉到真实文件
	name, files, err := m.TorrentFiles(metaGID)
	if err != nil {
		t.Fatal(err)
	}
	if name != "ubuntu.iso" {
		t.Fatalf("name = %q, want ubuntu.iso", name)
	}
	if len(files) != 1 || files[0].Path != "/data/bt/ubuntu.iso/iso.mkv" {
		t.Fatalf("files = %+v, want realgid 的真实文件", files)
	}

	// 迁移完成后(库里旧记录已被删),拿旧 gid 仍能解析
	m.sync()
	name, files, err = m.TorrentFiles(metaGID)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "/data/bt/ubuntu.iso/iso.mkv" {
		t.Fatalf("迁移后仍应解析到真实文件,得到 %+v", files)
	}

	// 勾选下发也要落到真实 gid 上
	if err := m.SelectFiles(metaGID, []int{1}); err != nil {
		t.Fatal(err)
	}
	if mock.lastSelect != "1" {
		t.Fatalf("select-file 应当下发给真实 gid,得到 %q", mock.lastSelect)
	}
	if len(mock.unpaused) != 1 || mock.unpaused[0] != "realgid" {
		t.Fatalf("应当 unpause realgid,得到 %v", mock.unpaused)
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

// 删任务时可以选择连文件一起删。文件清单要在 aria2 忘掉这个任务之前
// 拿到,BT 那种「一层种子名文件夹」删空之后也要收掉。
func TestRemoveTaskWithFiles(t *testing.T) {
	m, mock := newTestManager(t)

	task, err := m.Add("https://example.com/file.iso", "isos")
	if err != nil {
		t.Fatal(err)
	}
	// 造出 aria2 会报回来的那种绝对路径(dataRoot 是 /data)
	sub := filepath.Join(m.localDir, "isos", "种子名")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(sub, "a.mkv")
	for _, p := range []string{real, real + ".aria2"} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mock.statuses[task.GID] = &Status{
		GID: task.GID, Status: "complete",
		Files: []File{{Path: "/data/isos/种子名/a.mkv"}},
	}

	n, err := m.RemoveTask(task.GID, true)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("应当删掉 1 个条目,得到 %d", n)
	}
	if _, err := os.Stat(real); !os.IsNotExist(err) {
		t.Fatal("文件没删掉")
	}
	if _, err := os.Stat(real + ".aria2"); !os.IsNotExist(err) {
		t.Fatal(".aria2 控制文件没删掉")
	}
	if _, err := os.Stat(sub); !os.IsNotExist(err) {
		t.Fatal("空掉的种子文件夹应当一并收掉")
	}
	// 任务自己的保存目录要留着,它是用户选的
	if _, err := os.Stat(filepath.Join(m.localDir, "isos")); err != nil {
		t.Fatalf("不该删掉任务的保存目录: %v", err)
	}
}

// 不勾「删除文件」时只删记录
func TestRemoveTaskKeepsFiles(t *testing.T) {
	m, mock := newTestManager(t)
	task, err := m.Add("https://example.com/file.iso", "isos")
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(m.localDir, "isos", "a.mkv")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	mock.statuses[task.GID] = &Status{GID: task.GID, Status: "complete",
		Files: []File{{Path: "/data/isos/a.mkv"}}}

	if _, err := m.RemoveTask(task.GID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(real); err != nil {
		t.Fatalf("没勾删除文件时不该动文件: %v", err)
	}
}

// aria2 报回来的路径如果跑出了网盘目录,一概不删——这是删除操作唯一的
// 安全边界。
func TestLocalPathRejectsEscape(t *testing.T) {
	m, _ := newTestManager(t)
	for _, p := range []string{
		"/etc/passwd",
		"/data/../etc/passwd",
		"/datax/a.mkv",
		"",
	} {
		if got, ok := m.localPath(p); ok {
			t.Fatalf("%q 不该被接受,却翻成了 %q", p, got)
		}
	}
	if _, ok := m.localPath("/data/isos/a.mkv"); !ok {
		t.Fatal("网盘内的正常路径应当被接受")
	}
}

func TestMagnetHash(t *testing.T) {
	good := "magnet:?xt=urn:btih:" + strings.Repeat("ab", 20)
	if h, err := magnetHash(good); err != nil || h != strings.Repeat("ab", 20) {
		t.Fatalf("magnetHash(%q) = %q, %v; want 40 位小写 hex", good, h, err)
	}
	// 大写转小写
	if h, err := magnetHash("magnet:?xt=urn:btih:" + strings.Repeat("AB", 20)); err != nil || h != strings.Repeat("ab", 20) {
		t.Fatalf("大写 btih 应当转小写,得到 %q, %v", h, err)
	}
	// 后面还跟别的参数
	if h, err := magnetHash(good + "&dn=foo&tr=http://x/y"); err != nil || h != strings.Repeat("ab", 20) {
		t.Fatalf("带后续参数应只抠出 btih,得到 %q, %v", h, err)
	}
	for _, bad := range []string{
		"magnet:?dn=foo",                       // 没有 btih
		"magnet:?xt=urn:btih:abcdef",           // 不是 40 位
		"magnet:?xt=urn:btih:" + strings.Repeat("z", 40), // 不是 hex
	} {
		if _, err := magnetHash(bad); err == nil {
			t.Fatalf("magnetHash(%q) 应当报错", bad)
		}
	}
}

func TestWithTrackers(t *testing.T) {
	magnet := "magnet:?xt=urn:btih:" + strings.Repeat("ab", 20)
	out := withTrackers(magnet, "http://a.com/announce, udp://b.com/announce")
	if !strings.Contains(out, "&tr=http%3A%2F%2Fa.com%2Fannounce") ||
		!strings.Contains(out, "&tr=udp%3A%2F%2Fb.com%2Fannounce") {
		t.Fatalf("tracker 没有追加进磁力: %q", out)
	}
	if got := withTrackers(magnet, " , ,"); got != magnet {
		t.Fatalf("空白 tracker 应当跳过: %q", got)
	}
}

// 磁力转种子:finishMagnet 把 aria2 里的磁力元数据任务换成 .torrent 任务,
// 库记录迁到新 gid(follows=旧 gid),旧 gid 仍能解析,删除也照常工作。
func TestFinishMagnetMigrates(t *testing.T) {
	m, mock := newTestManager(t)

	old := "magnet:?xt=urn:btih:" + strings.Repeat("ab", 20)
	task, err := m.Add(old, "bt")
	if err != nil {
		t.Fatal(err)
	}
	metaGID := task.GID
	if len(mock.added) != 1 || mock.added[0] != old {
		t.Fatalf("磁力应先照旧交给 aria2,得到 %v", mock.added)
	}

	m.finishMagnet(metaGID, old, "bt", "ZmFrZXRvcnJlbnQ=", "some-movie")

	_, tasks := m.List()
	if len(tasks) != 1 {
		t.Fatalf("want 1 task, got %d", len(tasks))
	}
	nt := tasks[0]
	if nt.GID == metaGID {
		t.Fatalf("gid 应换成种子任务,还是旧 %q", metaGID)
	}
	if nt.Follows != metaGID {
		t.Fatalf("新记录应记 follows=%q,得到 %q", metaGID, nt.Follows)
	}
	if nt.Status != "paused" {
		t.Fatalf("转出的种子任务应为 paused,得到 %q", nt.Status)
	}
	if nt.URL != old {
		t.Fatalf("URL 应保留磁力,得到 %q", nt.URL)
	}
	if len(mock.torrentPaused) != 1 || mock.torrentPaused[0] != "true" {
		t.Fatalf("转出的种子任务要挂起等勾选: %v", mock.torrentPaused)
	}

	// 前端拿旧 gid 轮询/勾选/暂停/删除都要落到新任务上
	if cur := m.resolveGID(metaGID); cur != nt.GID {
		t.Fatalf("resolveGID(%q) = %q, want %q", metaGID, cur, nt.GID)
	}
	mock.statuses[nt.GID] = &Status{GID: nt.GID, Status: "paused"}
	if _, err := m.RemoveTask(metaGID, false); err != nil {
		t.Fatal(err)
	}
	_, tasks = m.List()
	if len(tasks) != 0 {
		t.Fatalf("按旧 gid 删除后应无任务,got %d", len(tasks))
	}
}

// 磁力转种子迁移旧记录删掉后,syncOne 再看到「gid 不存在」时不该把旧
// 记录复活成一条错误任务(GORM Save 按主键 upsert 会插回来)。
func TestSyncOneDoesNotResurrectMigratedMagnet(t *testing.T) {
	m, mock := newTestManager(t)

	old := "magnet:?xt=urn:btih:" + strings.Repeat("ef", 20)
	task, err := m.Add(old, "bt")
	if err != nil {
		t.Fatal(err)
	}
	metaGID := task.GID
	// aria2 视角:旧 gid 已经被删(比如 finishMagnet 里 Remove 掉)
	delete(mock.statuses, metaGID)

	// finishMagnet 迁移完成:旧记录没了,新记录 follows=旧 gid
	m.finishMagnet(metaGID, old, "bt", "ZmFrZXRvcnJlbnQ=", "m")
	if _, tasks := m.List(); len(tasks) != 1 {
		t.Fatalf("迁移后应有 1 条任务,got %d", len(tasks))
	}

	m.sync()
	_, tasks := m.List()
	if len(tasks) != 1 || tasks[0].GID == metaGID {
		t.Fatalf("旧磁力记录不应被复活,got %+v", tasks)
	}
	if tasks[0].Status != "paused" {
		t.Fatalf("新任务状态 = %q, want paused", tasks[0].Status)
	}
}

// 磁力已被 aria2 自己 follow(或用户已删除)时,finishMagnet 不该再迁移
// 出第二条任务。
func TestFinishMagnetNoOpWhenMigrated(t *testing.T) {
	m, mock := newTestManager(t)

	old := "magnet:?xt=urn:btih:" + strings.Repeat("cd", 20)
	task, err := m.Add(old, "bt")
	if err != nil {
		t.Fatal(err)
	}
	metaGID := task.GID

	// 先手动把旧记录迁走,模拟 aria2 followedBy 抢先
	m.db.Delete(&db.DownloadTask{}, "gid = ?", metaGID)
	nt := db.DownloadTask{
		GID: "realgid", URL: old, Dir: "bt", Status: "active", Follows: metaGID,
	}
	m.db.Create(&nt)

	m.finishMagnet(metaGID, old, "bt", "ZmFrZXRvcnJlbnQ=", "x")

	_, tasks := m.List()
	if len(tasks) != 1 || tasks[0].GID != "realgid" {
		t.Fatalf("已迁移的任务不应被再次替换,got %+v", tasks)
	}
	if len(mock.torrentPaused) != 0 {
		t.Fatalf("不该再往 aria2 里塞种子任务: %v", mock.torrentPaused)
	}
}
