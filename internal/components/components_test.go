package components

import (
	"os"
	"path/filepath"
	"testing"
)

// EnsureBundled 的关键性质:绝不覆盖已存在的副本。/config/bin 里的
// 组件可能已被用户在网页上升级过,启动时若拿镜像内置的旧版盖回去,
// 用户的升级就白做了。
func TestEnsureBundled(t *testing.T) {
	t.Run("不托管时什么都不做", func(t *testing.T) {
		dir := t.TempDir()
		m := New("", dir)
		m.EnsureBundled(Ytdlp)
		if _, err := os.Stat(filepath.Join(dir, "yt-dlp")); err == nil {
			t.Error("不该凭空创建文件")
		}
	})

	t.Run("从内置副本复制", func(t *testing.T) {
		bundled, binDir := t.TempDir(), t.TempDir()
		want := "#!/bin/sh\necho 2026.01.01\n"
		if err := os.WriteFile(filepath.Join(bundled, exeName("yt-dlp")),
			[]byte(want), 0o755); err != nil {
			t.Fatal(err)
		}
		m := New(binDir, bundled)
		m.EnsureBundled(Ytdlp)

		got, err := os.ReadFile(m.Path(Ytdlp))
		if err != nil {
			t.Fatalf("读取副本: %v", err)
		}
		if string(got) != want {
			t.Errorf("内容 = %q", got)
		}
		if _, err := os.Stat(m.Path(Ytdlp) + ".tmp"); err == nil {
			t.Error("临时文件没清掉")
		}
	})

	t.Run("已存在的不被覆盖", func(t *testing.T) {
		bundled, binDir := t.TempDir(), t.TempDir()
		os.WriteFile(filepath.Join(bundled, exeName("aria2c")), []byte("镜像内置的旧版"), 0o755)
		m := New(binDir, bundled)
		os.WriteFile(m.Path(Aria2), []byte("用户升级过的新版"), 0o755)

		m.EnsureBundled(Aria2)
		got, _ := os.ReadFile(m.Path(Aria2))
		if string(got) != "用户升级过的新版" {
			t.Fatalf("升级结果被镜像内置版覆盖了: %q", got)
		}
	})

	t.Run("镜像没内置的组件跳过", func(t *testing.T) {
		bundled, binDir := t.TempDir(), t.TempDir()
		m := New(binDir, bundled)
		m.EnsureBundled(FFmpeg) // bundled 里没有 ffmpeg
		if _, err := os.Stat(m.Path(FFmpeg)); err == nil {
			t.Error("不该创建出空文件")
		}
	})
}

func TestParseVersion(t *testing.T) {
	cases := []struct {
		kind Kind
		out  string
		want string
	}{
		{Ytdlp, "2026.01.15\n", "2026.01.15"},
		{Aria2, "aria2 version 1.37.0\nCopyright (C) 2006 ...\n", "1.37.0"},
		{FFmpeg, "ffmpeg version n7.1-31-g8b3f1d0 Copyright (c) 2000-2025\n", "n7.1-31-g8b3f1d0"},
		{FFmpeg, "ffmpeg version 6.1.1 Copyright (c) 2000-2023 the FFmpeg developers\n", "6.1.1"},
		// 意外的输出不能让整个状态查询崩掉
		{Aria2, "", ""},
		{Ytdlp, "", ""},
	}
	for _, c := range cases {
		if got := parseVersion(c.kind, c.out); got != c.want {
			t.Errorf("parseVersion(%s, %q) = %q, want %q", c.kind, c.out, got, c.want)
		}
	}
}

// 不托管模式(本机开发)下路径就是裸名字,交给 PATH 解析。
func TestPathFallback(t *testing.T) {
	m := New("", "")
	if m.Managed() {
		t.Error("binDir 为空时不该是托管模式")
	}
	if got := m.Path(Ytdlp); got != exeName("yt-dlp") {
		t.Errorf("Path = %q, want %q", got, exeName("yt-dlp"))
	}

	m2 := New(filepath.Join("C:", "config", "bin"), "")
	if !m2.Managed() {
		t.Error("配了 binDir 就该是托管模式")
	}
	if got := m2.Path(Aria2); got == exeName("aria2c") {
		t.Error("托管模式下应当返回完整路径")
	}
}

func TestFindAsset(t *testing.T) {
	rel := &ghRelease{Assets: []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	}{
		{Name: "aria2-x86_64-linux-musl_static.zip", URL: "https://example.test/x86"},
		{Name: "aria2-aarch64-linux-musl_static.zip", URL: "https://example.test/arm"},
	}}

	got, err := findAsset(rel, []string{"aria2-aarch64-linux-musl_static.zip"})
	if err != nil {
		t.Fatalf("findAsset: %v", err)
	}
	if got != "https://example.test/arm" {
		t.Errorf("URL = %q", got)
	}

	if _, err := findAsset(rel, []string{"不存在的产物.zip"}); err == nil {
		t.Error("找不到匹配产物时应当报错")
	}
}
