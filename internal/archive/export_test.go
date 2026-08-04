package archive

// 整盘导出 → 导入往返。这条路径出错会毁掉整个网盘,所以除了正常往返,
// 还要确认:密码不对拒绝、非本程序的包拒绝、版本不匹配拒绝、恶意路径拒绝。

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"pocketdrive/internal/db"
)

const testPassword = "test-password-123"

func exportBytes(t *testing.T, s *Service) []byte {
	t.Helper()
	req := httptest.NewRequest("GET", "/api/v1/admin/export", nil)
	w := httptest.NewRecorder()
	s.HandleExport(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("导出 → %d: %s", w.Code, w.Body)
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	return w.Body.Bytes()
}

func doImport(t *testing.T, s *Service, pkg []byte, password string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/admin/import", bytes.NewReader(pkg))
	req.Header.Set("X-PD-Password", password)
	w := httptest.NewRecorder()
	s.HandleImport(w, req)
	return w.Code, w.Body.String()
}

// tarEntries 列出包内所有条目名。
func tarEntries(t *testing.T, pkg []byte) []string {
	t.Helper()
	gr, err := gzip.NewReader(bytes.NewReader(pkg))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer gr.Close()
	var out []string
	tr := tar.NewReader(gr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		out = append(out, h.Name)
	}
	sort.Strings(out)
	return out
}

func TestExportImportRoundTrip(t *testing.T) {
	src := testSvc(t)
	writeFile(t, src, "笔记.md", "# 我的笔记\n")
	writeFile(t, src, "音乐/歌 song.mp3", "FAKE-MP3-DATA")
	writeFile(t, src, "照片/2026/风景.jpg", "FAKE-JPG")
	// 配置库里也放点东西,验证它确实被带走了
	src.db.Create(&db.FolderIcon{Path: "音乐", Icon: "🎵"})

	pkg := exportBytes(t, src)

	t.Run("包内含 manifest、配置库和网盘文件", func(t *testing.T) {
		names := tarEntries(t, pkg)
		joined := strings.Join(names, "\n")
		for _, want := range []string{
			manifestName, "test.db",
			"data/笔记.md", "data/音乐/歌 song.mp3", "data/照片/2026/风景.jpg",
		} {
			if !strings.Contains(joined, want) {
				t.Errorf("包里缺 %s\n实际: %v", want, names)
			}
		}
	})

	t.Run("manifest 内容正确", func(t *testing.T) {
		gr, _ := gzip.NewReader(bytes.NewReader(pkg))
		defer gr.Close()
		tr := tar.NewReader(gr)
		h, err := tr.Next()
		if err != nil {
			t.Fatal(err)
		}
		// manifest 必须是第一个条目,导入时才能先做版本校验
		if h.Name != manifestName {
			t.Fatalf("第一个条目 = %q, want %q", h.Name, manifestName)
		}
		var m manifest
		if err := json.NewDecoder(tr).Decode(&m); err != nil {
			t.Fatal(err)
		}
		if m.Version != manifestVersion || m.App != "PocketDrive" {
			t.Errorf("manifest = %+v", m)
		}
		if m.Files != 3 {
			t.Errorf("文件数 = %d, want 3", m.Files)
		}
		if m.ExportedAt.IsZero() || time.Since(m.ExportedAt) > time.Hour {
			t.Errorf("导出时间不对: %v", m.ExportedAt)
		}
	})

	t.Run("导入到一个空实例", func(t *testing.T) {
		dst := testSvc(t)
		code, body := doImport(t, dst, pkg, testPassword)
		if code != http.StatusOK {
			t.Fatalf("导入 → %d: %s", code, body)
		}
		if !strings.Contains(body, `"files":3`) {
			t.Errorf("应恢复 3 个文件: %s", body)
		}
		if c := readFile(t, dst, "笔记.md"); c != "# 我的笔记\n" {
			t.Errorf("笔记内容 = %q", c)
		}
		if c := readFile(t, dst, "音乐/歌 song.mp3"); c != "FAKE-MP3-DATA" {
			t.Errorf("音乐内容 = %q", c)
		}
		if c := readFile(t, dst, "照片/2026/风景.jpg"); c != "FAKE-JPG" {
			t.Errorf("深层文件 = %q", c)
		}
		// 配置库落在旁边等重启顶替,不能当场覆盖正在用的库
		if _, err := os.Stat(dst.dbPath + importedSuffix); err != nil {
			t.Errorf("导入的配置库没落地: %v", err)
		}
	})
}

func TestImportRejects(t *testing.T) {
	src := testSvc(t)
	writeFile(t, src, "a.txt", "x")
	pkg := exportBytes(t, src)

	t.Run("密码不对", func(t *testing.T) {
		dst := testSvc(t)
		code, body := doImport(t, dst, pkg, "错误密码")
		if code != http.StatusForbidden {
			t.Fatalf("→ %d, want 403: %s", code, body)
		}
		if _, err := dst.files.Root().Stat("a.txt"); err == nil {
			t.Error("密码错误却写入了文件")
		}
	})

	t.Run("空密码", func(t *testing.T) {
		dst := testSvc(t)
		if code, _ := doImport(t, dst, pkg, ""); code != http.StatusForbidden {
			t.Errorf("→ %d, want 403", code)
		}
	})

	t.Run("不是 gzip", func(t *testing.T) {
		dst := testSvc(t)
		code, _ := doImport(t, dst, []byte("这不是压缩包"), testPassword)
		if code != http.StatusBadRequest {
			t.Errorf("→ %d, want 400", code)
		}
	})

	t.Run("没有 manifest 的 tar.gz", func(t *testing.T) {
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gw)
		body := []byte("hi")
		tw.WriteHeader(&tar.Header{
			Name: "data/x.txt", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg,
		})
		tw.Write(body)
		tw.Close()
		gw.Close()

		dst := testSvc(t)
		code, resp := doImport(t, dst, buf.Bytes(), testPassword)
		if code != http.StatusBadRequest {
			t.Fatalf("→ %d, want 400", code)
		}
		if !strings.Contains(resp, "manifest") {
			t.Errorf("错误信息应提到 manifest: %s", resp)
		}
	})

	t.Run("版本不兼容", func(t *testing.T) {
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gw)
		mf, _ := json.Marshal(manifest{Version: manifestVersion + 1, App: "PocketDrive"})
		writeTarBytes(tw, manifestName, mf)
		tw.Close()
		gw.Close()

		dst := testSvc(t)
		code, resp := doImport(t, dst, buf.Bytes(), testPassword)
		if code != http.StatusBadRequest {
			t.Fatalf("→ %d, want 400", code)
		}
		if !strings.Contains(resp, "不兼容") {
			t.Errorf("错误信息应说明版本不兼容: %s", resp)
		}
	})

	t.Run("包内含越界路径", func(t *testing.T) {
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gw)
		mf, _ := json.Marshal(manifest{Version: manifestVersion, App: "PocketDrive"})
		writeTarBytes(tw, manifestName, mf)
		body := []byte("evil")
		tw.WriteHeader(&tar.Header{
			Name: "data/../../../被入侵.txt", Mode: 0o644,
			Size: int64(len(body)), Typeflag: tar.TypeReg,
		})
		tw.Write(body)
		tw.Close()
		gw.Close()

		dst := testSvc(t)
		code, resp := doImport(t, dst, buf.Bytes(), testPassword)
		if code != http.StatusBadRequest {
			t.Fatalf("→ %d, want 400: %s", code, resp)
		}
	})
}

// 待恢复的库要在启动时顶替正式库,且不能把旧库的 WAL 残留带过去。
func TestRestorePendingImport(t *testing.T) {
	t.Run("没有待恢复的库时什么都不做", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "pd.db")
		os.WriteFile(p, []byte("原库"), 0o600)
		if err := RestorePendingImport(p); err != nil {
			t.Fatalf("RestorePendingImport: %v", err)
		}
		got, _ := os.ReadFile(p)
		if string(got) != "原库" {
			t.Errorf("库被动了: %q", got)
		}
	})

	t.Run("顶替并清掉旧 WAL", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "pd.db")
		os.WriteFile(p, []byte("原库"), 0o600)
		os.WriteFile(p+"-wal", []byte("旧 WAL"), 0o600)
		os.WriteFile(p+"-shm", []byte("旧 SHM"), 0o600)
		os.WriteFile(p+importedSuffix, []byte("导入的库"), 0o600)

		if err := RestorePendingImport(p); err != nil {
			t.Fatalf("RestorePendingImport: %v", err)
		}
		got, _ := os.ReadFile(p)
		if string(got) != "导入的库" {
			t.Fatalf("库 = %q, want 导入的库", got)
		}
		for _, suffix := range []string{"-wal", "-shm", importedSuffix} {
			if _, err := os.Stat(p + suffix); err == nil {
				t.Errorf("%s 应当被清掉", suffix)
			}
		}
	})
}
