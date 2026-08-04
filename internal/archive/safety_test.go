package archive

import (
	"testing"
)

// 压缩包是不可信输入。条目名带 ../ 就能让解压覆盖数据目录外的文件
// (Zip Slip),必须在写盘前挡掉。
func TestSafeName(t *testing.T) {
	bad := []struct {
		name string
		in   string
	}{
		{"父目录穿越", "../evil.sh"},
		{"多级穿越", "../../../../etc/passwd"},
		{"中间穿越回退到上层", "a/b/../../../evil"},
		{"绝对路径", "/etc/passwd"},
		{"反斜杠绝对路径", "\\windows\\system32\\x.dll"},
		{"盘符路径", "C:/Windows/evil.dll"},
		{"反斜杠穿越", "..\\..\\evil"},
		{"纯父目录", ".."},
	}
	for _, c := range bad {
		t.Run(c.name+"应被拒绝", func(t *testing.T) {
			got, err := safeName(c.in)
			if err == nil {
				t.Errorf("safeName(%q) = %q, 应当报错", c.in, got)
			}
		})
	}

	ok := []struct {
		in, want string
	}{
		{"a.txt", "a.txt"},
		{"dir/a.txt", "dir/a.txt"},
		{"./a.txt", "a.txt"},
		{"dir/./sub/a.txt", "dir/sub/a.txt"},
		{"dir/sub/../a.txt", "dir/a.txt"}, // 回退但没越界
		{"中文 目录/文件 名.txt", "中文 目录/文件 名.txt"},
		{"a\\b.txt", "a/b.txt"}, // Windows 分隔符归一
	}
	for _, c := range ok {
		t.Run("放行 "+c.in, func(t *testing.T) {
			got, err := safeName(c.in)
			if err != nil {
				t.Fatalf("safeName(%q) 报错: %v", c.in, err)
			}
			if got != c.want {
				t.Errorf("safeName(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}

	t.Run("当前目录被忽略", func(t *testing.T) {
		got, err := safeName(".")
		if err != nil || got != "" {
			t.Errorf(`safeName(".") = %q, %v; want "", nil`, got, err)
		}
	})
}

// 压缩炸弹:极小的包解出天量数据,不设上限会把磁盘写满。
func TestExtractGuard(t *testing.T) {
	t.Run("总量超限", func(t *testing.T) {
		g := &extractGuard{}
		if err := g.add(maxExtractBytes - 1); err != nil {
			t.Fatalf("正常大小不该报错: %v", err)
		}
		if err := g.add(2); err == nil {
			t.Error("超过上限应当报错")
		}
	})

	t.Run("条目数超限", func(t *testing.T) {
		g := &extractGuard{entries: maxExtractEntries}
		if err := g.entry(); err == nil {
			t.Error("超过条目上限应当报错")
		}
	})
}

func TestFormatFromName(t *testing.T) {
	cases := map[string]string{
		"a.zip":       formatZip,
		"a.ZIP":       formatZip,
		"a.tar.gz":    formatTarGz,
		"a.tgz":       formatTarGz,
		"a.tar.xz":    formatTarXz,
		"a.txz":       formatTarXz,
		"a.tar":       formatTar,
		"备份 2026.zip": formatZip,
		"a.rar":       "",
		"a.7z":        "",
		"a.txt":       "",
		"noext":       "",
	}
	for in, want := range cases {
		if got := formatFromName(in); got != want {
			t.Errorf("formatFromName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSameStore(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"a/b.txt", "c/d.zip", true},         // 都在本机
		{"", "x.zip", true},                  // 本机根
		{"@R2/a.txt", "@R2/b.zip", true},     // 同一挂载
		{"@R2/a/b.txt", "@R2/x.zip", true},   // 同一挂载不同层级
		{"@R2/a.txt", "@S3/b.zip", false},    // 不同挂载
		{"@R2/a.txt", "b.zip", false},        // 挂载 vs 本机
		{"a.txt", "@R2/b.zip", false},        // 本机 vs 挂载
		{"@R2/a.txt", "@R2foo/b.zip", false}, // 名字前缀相同但不是同一个
	}
	for _, c := range cases {
		if got := sameStore(c.a, c.b); got != c.want {
			t.Errorf("sameStore(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
