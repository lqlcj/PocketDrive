package archive

// 压缩 → 解压往返:验证内容、层级、中文名都能原样回来,以及带 ../ 的
// 恶意包在真实解压流程里确实写不出目标目录。

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"pocketdrive/internal/auth"
	"pocketdrive/internal/cloud"
	"pocketdrive/internal/db"
	"pocketdrive/internal/files"
)

func testSvc(t *testing.T) *Service {
	t.Helper()
	tmp := t.TempDir()
	gdb, err := db.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := gdb.DB(); err == nil {
			sqlDB.Close()
		}
	})
	fs, err := files.New(filepath.Join(tmp, "data"), filepath.Join(tmp, "uploads"),
		cloud.New(gdb), gdb)
	if err != nil {
		t.Fatalf("files.New: %v", err)
	}
	t.Cleanup(func() { fs.Root().Close() })
	authSvc, err := auth.New(gdb, "admin", "test-password-123", tmp)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	return New(gdb, fs, cloud.New(gdb), authSvc, filepath.Join(tmp, "test.db"), "test")
}

func writeFile(t *testing.T, s *Service, p, content string) {
	t.Helper()
	if dir := path.Dir(p); dir != "." {
		if err := s.files.Root().MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f, err := s.files.Root().Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func readFile(t *testing.T, s *Service, p string) string {
	t.Helper()
	f, err := s.files.Root().Open(p)
	if err != nil {
		t.Fatalf("打开 %s: %v", p, err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// tree 列出数据目录下的所有文件(相对路径,已排序)。
func tree(t *testing.T, s *Service, root string) []string {
	t.Helper()
	var out []string
	err := fs.WalkDir(s.files.Root().FS(), root, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			out = append(out, strings.TrimPrefix(strings.TrimPrefix(name, root), "/"))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 %s: %v", root, err)
	}
	sort.Strings(out)
	return out
}

func TestCompressExtractRoundTrip(t *testing.T) {
	for _, format := range []string{formatZip, formatTarGz} {
		t.Run(format, func(t *testing.T) {
			s := testSvc(t)
			writeFile(t, s, "源/说明.txt", "顶层文件\n")
			writeFile(t, s, "源/子目录/数据.csv", "a,b\n1,2\n")
			writeFile(t, s, "源/子目录/更深/备注 note.md", "# 标题\n")

			pkg := "备份." + format
			ct := &db.ArchiveTask{
				Kind: "compress", Status: "running", Format: format,
				Src: "源", Dest: pkg,
			}
			if err := s.db.Create(ct).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.runCompress(context.Background(), ct); err != nil {
				t.Fatalf("压缩: %v", err)
			}

			et := &db.ArchiveTask{
				Kind: "extract", Status: "running", Format: format,
				Src: pkg, Dest: "还原",
			}
			if err := s.db.Create(et).Error; err != nil {
				t.Fatal(err)
			}
			if err := s.runExtract(context.Background(), et); err != nil {
				t.Fatalf("解压: %v", err)
			}

			want := []string{
				"源/子目录/更深/备注 note.md",
				"源/子目录/数据.csv",
				"源/说明.txt",
			}
			got := tree(t, s, "还原")
			sort.Strings(want)
			if strings.Join(got, "|") != strings.Join(want, "|") {
				t.Fatalf("解压结果 = %v\nwant %v", got, want)
			}
			if c := readFile(t, s, "还原/源/说明.txt"); c != "顶层文件\n" {
				t.Errorf("内容 = %q", c)
			}
			if c := readFile(t, s, "还原/源/子目录/更深/备注 note.md"); c != "# 标题\n" {
				t.Errorf("深层文件内容 = %q", c)
			}
		})
	}
}

func TestCompressMultipleSources(t *testing.T) {
	s := testSvc(t)
	writeFile(t, s, "甲/1.txt", "一")
	writeFile(t, s, "乙/1.txt", "二") // 同名,靠顶层目录区分

	ct := &db.ArchiveTask{
		Kind: "compress", Status: "running", Format: formatZip,
		Src: "甲|乙", Dest: "两个.zip",
	}
	s.db.Create(ct)
	if err := s.runCompress(context.Background(), ct); err != nil {
		t.Fatalf("压缩: %v", err)
	}
	et := &db.ArchiveTask{
		Kind: "extract", Status: "running", Format: formatZip,
		Src: "两个.zip", Dest: "out",
	}
	s.db.Create(et)
	if err := s.runExtract(context.Background(), et); err != nil {
		t.Fatalf("解压: %v", err)
	}
	if c := readFile(t, s, "out/甲/1.txt"); c != "一" {
		t.Errorf("甲 = %q", c)
	}
	if c := readFile(t, s, "out/乙/1.txt"); c != "二" {
		t.Errorf("乙 = %q", c)
	}
}

// 真实解压流程必须挡住 Zip Slip,而不只是 safeName 单元测试通过。
func TestExtractRejectsZipSlip(t *testing.T) {
	t.Run("zip", func(t *testing.T) {
		s := testSvc(t)
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create("../../被入侵.txt")
		w.Write([]byte("evil"))
		zw.Close()
		writeFile(t, s, "恶意.zip", buf.String())

		et := &db.ArchiveTask{
			Kind: "extract", Status: "running", Format: formatZip,
			Src: "恶意.zip", Dest: "out",
		}
		s.db.Create(et)
		err := s.runExtract(context.Background(), et)
		if err == nil {
			t.Fatal("带 ../ 的条目应当被拒绝")
		}
		if !strings.Contains(err.Error(), "越界") {
			t.Errorf("错误信息应说明原因: %v", err)
		}
	})

	t.Run("tar.gz", func(t *testing.T) {
		s := testSvc(t)
		var buf bytes.Buffer
		gw := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gw)
		body := []byte("evil")
		tw.WriteHeader(&tar.Header{
			Name: "../../被入侵.txt", Mode: 0o644,
			Size: int64(len(body)), Typeflag: tar.TypeReg,
		})
		tw.Write(body)
		tw.Close()
		gw.Close()
		writeFile(t, s, "恶意.tar.gz", buf.String())

		et := &db.ArchiveTask{
			Kind: "extract", Status: "running", Format: formatTarGz,
			Src: "恶意.tar.gz", Dest: "out",
		}
		s.db.Create(et)
		if err := s.runExtract(context.Background(), et); err == nil {
			t.Fatal("带 ../ 的条目应当被拒绝")
		}
	})
}

// tar 里的符号链接可以指到数据目录外,是另一条绕过路径限制的通道。
func TestExtractSkipsSymlinks(t *testing.T) {
	s := testSvc(t)
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	tw.WriteHeader(&tar.Header{
		Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd", Mode: 0o777,
	})
	body := []byte("正常内容")
	tw.WriteHeader(&tar.Header{
		Name: "正常.txt", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg,
	})
	tw.Write(body)
	tw.Close()
	gw.Close()
	writeFile(t, s, "带链接.tar.gz", buf.String())

	et := &db.ArchiveTask{
		Kind: "extract", Status: "running", Format: formatTarGz,
		Src: "带链接.tar.gz", Dest: "out",
	}
	s.db.Create(et)
	if err := s.runExtract(context.Background(), et); err != nil {
		t.Fatalf("解压: %v", err)
	}
	got := tree(t, s, "out")
	if len(got) != 1 || got[0] != "正常.txt" {
		t.Fatalf("解压结果 = %v, want [正常.txt](链接被跳过)", got)
	}
}
