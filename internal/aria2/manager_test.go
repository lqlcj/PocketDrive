package aria2

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"pocketdrive/internal/db"
)

// mockAria2 is a minimal aria2 JSON-RPC server for regression tests.
type mockAria2 struct {
	t             *testing.T
	statuses      map[string]*Status // gid -> status
	added         []string           // uris received by addUri
	addPaused     []string           // pause option received by addUri
	addPauseMeta  []string           // pause-metadata option received by addUri
	dirs          []string
	torrentPaused []string // pause option received by addTorrent
	lastSelect    string   // select-file passed to changeOption
	unpaused      []string // gids passed to aria2.unpause
	removed       []string // gids passed to aria2.remove
	nextGID       int
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
		m.addPaused = append(m.addPaused, opts["pause"])
		m.addPauseMeta = append(m.addPauseMeta, opts["pause-metadata"])
		gid := nextGID()
		m.statuses[gid] = &Status{GID: gid, Status: "active"}
		if opts["pause"] == "true" {
			m.statuses[gid].Status = "paused"
		}
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
	case "aria2.pause", "aria2.removeDownloadResult":
		write("ok")
	case "aria2.remove":
		var gid string
		_ = json.Unmarshal(req.Params[1], &gid)
		m.removed = append(m.removed, gid)
		delete(m.statuses, gid)
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
			{Path: "/data/bt/mytorrent/a.mkv", Length: "100", Selected: "true"},
			{Path: "/data/bt/mytorrent/b.mp4", Length: "200", Selected: "false"},
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
	if files[0].Index != 1 || files[0].Length != 100 || !files[0].Selected {
		t.Fatalf("files[0] = %+v, want index 1 / length 100", files[0])
	}
	if files[1].Index != 2 || files[1].Length != 200 || files[1].Selected {
		t.Fatalf("files[1] = %+v, want index 2 / length 200", files[1])
	}
}

func TestMagnetFollowedByMigration(t *testing.T) {
	m, mock := newTestManager(t)

	magnet := "magnet:?xt=urn:btih:" + strings.Repeat("ab", 20)
	task, err := m.Add(magnet, "bt")
	if err != nil {
		t.Fatal(err)
	}
	metaGID := task.GID

	// 元数据下载完成,真实下载迁移到新 gid
	mock.statuses["realgid"] = &Status{
		GID: "realgid", Status: "paused",
		TotalLength: "2000", CompletedLength: "0",
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
	if tasks[0].Status != "paused" {
		t.Fatalf("pause-metadata 生成的真实任务应保持 paused,得到 %q", tasks[0].Status)
	}
}

// 磁力元数据就绪后,前端仍拿着旧 gid 轮询文件清单:resolveGID 必须把它
// 翻到 followedBy 的真实 gid,否则永远只看到 [METADATA] 伪文件、弹框
// 一直转圈(「上传的种子能加载」正是因为它没有 follow 这一步)。
func TestMagnetTorrentFilesViaOldGID(t *testing.T) {
	m, mock := newTestManager(t)

	magnet := "magnet:?xt=urn:btih:" + strings.Repeat("cd", 20)
	task, err := m.Add(magnet, "bt")
	if err != nil {
		t.Fatal(err)
	}
	metaGID := task.GID

	mock.statuses["realgid"] = &Status{
		GID: "realgid", Status: "paused",
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
		Files: []File{{Path: "/data/bt/[METADATA]" + magnet, Length: "0"}},
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
			"./data:/data",
		},
		{"Download aborted.", "docker compose logs aria2"},
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

func TestTrackerParsingAndTaskOptions(t *testing.T) {
	m, _ := newTestManager(t)
	s := defaultSettings()
	s.CustomTrackers = "UDP://TRACKER.EXAMPLE.COM:6969/announce\n" +
		"https://tracker.example.com/announce, udp://tracker.example.com:6969/announce"
	if err := validateSettings(&s); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(s.CustomTrackers, "\n") + 1; got != 2 {
		t.Fatalf("自定义 Tracker 应去重为 2 条,got %d: %q", got, s.CustomTrackers)
	}
	badSettings := defaultSettings()
	badSettings.CustomTrackers = "ftp://tracker.example.com/announce"
	if validateSettings(&badSettings) == nil {
		t.Fatal("不受 aria2 支持的 Tracker 协议应被拒绝")
	}

	auto := "udp://auto.example.com:80/announce\nhttps://tracker.example.com/announce"
	if err := m.db.Save(&db.Setting{Key: trackersKey, Value: auto}).Error; err != nil {
		t.Fatal(err)
	}
	if err := m.saveSettings(s); err != nil {
		t.Fatal(err)
	}
	opts := m.taskOpts("", true)
	list, err := parseTrackerText(opts["bt-tracker"], true)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("自动+自定义 Tracker 应去重合并为 3 条,got %v", list)
	}

	s.TrackerAuto = false
	if err := m.saveSettings(s); err != nil {
		t.Fatal(err)
	}
	list, err = parseTrackerText(m.taskOpts("", true)["bt-tracker"], true)
	if err != nil || len(list) != 2 {
		t.Fatalf("关闭自动更新后仍应使用 2 条自定义 Tracker,got %v,err=%v", list, err)
	}
}

func TestBootstrapTrackersBeforeFirstRefresh(t *testing.T) {
	m, _ := newTestManager(t)
	if got := m.autoTrackerList(); len(got) != len(bootstrapTrackers) {
		t.Fatalf("首次更新前应使用 %d 条启动 Tracker,got %v", len(bootstrapTrackers), got)
	}
	list, err := parseTrackerText(m.taskOpts("", true)["bt-tracker"], true)
	if err != nil || len(list) != len(bootstrapTrackers) {
		t.Fatalf("首次磁力任务应注入启动 Tracker,got %v,err=%v", list, err)
	}
}

func TestUpdateTrackersFallbackAndPreserveCache(t *testing.T) {
	m, _ := newTestManager(t)
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "temporary", http.StatusBadGateway)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Join([]string{
			"# best trackers",
			"udp://tracker-one.example:80/announce",
			"https://tracker-two.example/announce",
			"http://tracker-three.example/announce",
			"udp://tracker-one.example:80/announce",
			"not-a-tracker",
		}, "\n")))
	}))
	defer good.Close()

	m.trackerSources = []string{bad.URL, good.URL}
	n, source, err := m.UpdateTrackers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 || source != good.URL {
		t.Fatalf("应从第二个源得到 3 条,got n=%d source=%q", n, source)
	}
	if got := len(m.autoTrackerList()); got != 3 {
		t.Fatalf("缓存条数=%d,want 3", got)
	}
	if got := m.getSetting(trackersSourceKey); got != good.URL {
		t.Fatalf("成功来源未记录: %q", got)
	}

	m.trackerSources = []string{bad.URL}
	if _, _, err := m.UpdateTrackers(context.Background()); err == nil {
		t.Fatal("所有源失败时应返回错误")
	}
	if got := len(m.autoTrackerList()); got != 3 {
		t.Fatalf("更新失败不应覆盖旧缓存,got %d", got)
	}
	if got := m.getSetting(trackersErrorKey); !strings.Contains(got, "所有 Tracker 更新源均不可用") {
		t.Fatalf("最近错误未记录: %q", got)
	}
}

func TestConcurrentAutomaticTrackerRefreshOnlyFetchesOnce(t *testing.T) {
	m, _ := newTestManager(t)
	var hits atomic.Int32
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(strings.Join([]string{
			"udp://tracker-one.example:80/announce",
			"https://tracker-two.example/announce",
			"http://tracker-three.example/announce",
		}, "\n")))
	}))
	defer source.Close()
	m.trackerSources = []string{source.URL}

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	for range 2 {
		go func() {
			defer wg.Done()
			<-start
			m.refreshTrackersIfStale()
		}()
	}
	close(start)
	wg.Wait()

	if got := hits.Load(); got != 1 {
		t.Fatalf("并发自动更新应只请求一次 Tracker 源,got %d", got)
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

func TestAddRollsBackAria2WhenDBWriteFails(t *testing.T) {
	m, mock := newTestManager(t)
	sqlDB, err := m.db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := m.Add("https://example.com/orphan.bin", ""); err == nil {
		t.Fatal("数据库写入失败时 Add 应返回错误")
	}
	if len(mock.removed) != 1 || mock.removed[0] != "gid1" {
		t.Fatalf("数据库写入失败后应撤销 aria2 任务,removed=%v", mock.removed)
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
	// 32 位 Base32 是 BEP 9 允许的常见写法,统一转成 40 位 hex。
	base32Magnet := "magnet:?xt=urn:btih:" + strings.Repeat("A", 32)
	if h, err := magnetHash(base32Magnet); err != nil || h != strings.Repeat("00", 20) {
		t.Fatalf("Base32 btih 转换 = %q, %v; want 40 个 0", h, err)
	}
	// 后面还跟别的参数
	if h, err := magnetHash(good + "&dn=foo&tr=http://x/y"); err != nil || h != strings.Repeat("ab", 20) {
		t.Fatalf("带后续参数应只抠出 btih,得到 %q, %v", h, err)
	}
	for _, bad := range []string{
		"magnet:?dn=foo",                                 // 没有 btih
		"magnet:?xt=urn:btih:abcdef",                     // 不是 40 位
		"magnet:?xt=urn:btih:" + strings.Repeat("z", 40), // 不是 hex
	} {
		if _, err := magnetHash(bad); err == nil {
			t.Fatalf("magnetHash(%q) 应当报错", bad)
		}
	}
}

// 磁力元数据任务必须运行，但 aria2 根据元数据生成的真实下载必须暂停。
// pause=true 会把元数据任务本身也停掉；pause-metadata=true 才是这里需要的语义。
func TestAddMagnetUsesPauseMetadata(t *testing.T) {
	m, mock := newTestManager(t)

	magnet := "magnet:?xt=urn:btih:" + strings.Repeat("12", 20)
	task, err := m.Add(magnet, "bt")
	if err != nil {
		t.Fatal(err)
	}
	if len(mock.addPauseMeta) != 1 || mock.addPauseMeta[0] != "true" {
		t.Fatalf("磁力任务应带 pause-metadata=true 提交,得到 %v", mock.addPauseMeta)
	}
	if mock.addPaused[0] != "" {
		t.Fatalf("磁力元数据任务不能带 pause=true,得到 %q", mock.addPaused[0])
	}
	if task.Status != "active" {
		t.Fatalf("磁力元数据任务落库应为 active,得到 %q", task.Status)
	}

	// Base32 磁力同样走 aria2 原生元数据流程。
	base32Magnet := "magnet:?xt=urn:btih:" + strings.Repeat("A", 32)
	base32Task, err := m.Add(base32Magnet, "bt")
	if err != nil {
		t.Fatal(err)
	}
	if mock.addPauseMeta[1] != "true" || mock.addPaused[1] != "" || base32Task.Status != "active" {
		t.Fatalf("Base32 磁力选项不正确: pause-metadata=%q pause=%q status=%q",
			mock.addPauseMeta[1], mock.addPaused[1], base32Task.Status)
	}

	// 明显无效的 btih 直接拒绝，不进入 aria2 队列。
	before := len(mock.added)
	if _, err := m.Add("magnet:?xt=urn:btih:abcdef", "bt"); err == nil {
		t.Fatal("非法 btih 磁力应当被拒绝")
	}
	if len(mock.added) != before {
		t.Fatal("非法磁力不应提交给 aria2")
	}

	// 普通直链不受影响
	if _, err := m.Add("https://a.com/f.iso", ""); err != nil {
		t.Fatal(err)
	}
	if mock.addPaused[2] != "" || mock.addPauseMeta[2] != "" {
		t.Fatalf("直链不该带 BT 暂停选项,pause=%q pause-metadata=%q",
			mock.addPaused[2], mock.addPauseMeta[2])
	}
}
