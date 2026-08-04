package components

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// EnsureBundled 的关键性质:绝不覆盖已存在的副本。/config/bin 里的
// yt-dlp 可能已被用户在网页上升级过,启动时若拿镜像内置的旧版盖回去,
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
		os.WriteFile(filepath.Join(bundled, exeName("yt-dlp")), []byte("镜像内置的旧版"), 0o755)
		m := New(binDir, bundled)
		os.WriteFile(m.Path(Ytdlp), []byte("用户升级过的新版"), 0o755)

		m.EnsureBundled(Ytdlp)
		got, _ := os.ReadFile(m.Path(Ytdlp))
		if string(got) != "用户升级过的新版" {
			t.Fatalf("升级结果被镜像内置版覆盖了: %q", got)
		}
	})

	t.Run("镜像没内置就跳过", func(t *testing.T) {
		bundled, binDir := t.TempDir(), t.TempDir()
		m := New(binDir, bundled) // bundled 是空目录
		m.EnsureBundled(Ytdlp)
		if _, err := os.Stat(m.Path(Ytdlp)); err == nil {
			t.Error("不该创建出空文件")
		}
	})

	// aria2 在别的容器里、ffmpeg 随镜像走,都不该被搬进 volume:
	// 搬进去就等于凭空多出一份永远不会被更新的副本
	t.Run("非托管组件一概不碰", func(t *testing.T) {
		bundled, binDir := t.TempDir(), t.TempDir()
		os.WriteFile(filepath.Join(bundled, exeName("ffmpeg")), []byte("x"), 0o755)
		os.WriteFile(filepath.Join(bundled, exeName("aria2")), []byte("x"), 0o755)
		m := New(binDir, bundled)
		m.EnsureBundled(Ytdlp, Aria2, FFmpeg)

		if _, err := os.Stat(filepath.Join(binDir, exeName("ffmpeg"))); err == nil {
			t.Error("ffmpeg 不该被复制进 volume")
		}
		if _, err := os.Stat(filepath.Join(binDir, exeName("aria2"))); err == nil {
			t.Error("aria2 不该被复制进 volume")
		}
	})
}

// 渠道决定了网页上能对组件做什么,错了会让用户对着一个假按钮点。
func TestChannel(t *testing.T) {
	m := New(filepath.Join(t.TempDir(), "bin"), "")
	cases := []struct {
		k    Kind
		want Channel
	}{
		{Ytdlp, ChanManaged},
		{Aria2, ChanSidecar},
		{FFmpeg, ChanImage},
	}
	for _, c := range cases {
		if got := m.Channel(c.k); got != c.want {
			t.Errorf("Channel(%s) = %q, want %q", c.k, got, c.want)
		}
	}
	if !m.Managed(Ytdlp) || m.Managed(Aria2) || m.Managed(FFmpeg) {
		t.Error("只有 yt-dlp 该是网页可升级的")
	}

	// 本机开发:没有 binDir,一切看 PATH
	dev := New("", "")
	for _, k := range []Kind{Ytdlp, Aria2, FFmpeg} {
		if got := dev.Channel(k); got != ChanSystem {
			t.Errorf("本机开发 Channel(%s) = %q, want %q", k, got, ChanSystem)
		}
	}
}

func TestPath(t *testing.T) {
	dev := New("", "")
	if got := dev.Path(Ytdlp); got != exeName("yt-dlp") {
		t.Errorf("本机开发 Path = %q, want %q", got, exeName("yt-dlp"))
	}

	binDir := filepath.Join(t.TempDir(), "bin")
	m := New(binDir, "")
	if got := m.Path(Ytdlp); got != filepath.Join(binDir, exeName("yt-dlp")) {
		t.Errorf("托管模式 Path = %q", got)
	}
	// ffmpeg 在镜像里,交给 PATH
	if got := m.Path(FFmpeg); got != exeName("ffmpeg") {
		t.Errorf("Path(ffmpeg) = %q, want %q", got, exeName("ffmpeg"))
	}
	// aria2 压根不在本容器内
	if got := m.Path(Aria2); got != "" {
		t.Errorf("Path(aria2) = %q, want 空", got)
	}
}

// 网页只管得了 yt-dlp;对另外两个必须明确拒绝,而不是假装装上了。
func TestInstallRejectsUnmanaged(t *testing.T) {
	m := New(filepath.Join(t.TempDir(), "bin"), "")
	for _, k := range []Kind{Aria2, FFmpeg} {
		if err := m.Install(context.Background(), k); err == nil {
			t.Errorf("Install(%s) 应当报错", k)
		}
	}
	// 本机开发下连 yt-dlp 也不能装
	dev := New("", "")
	if err := dev.Install(context.Background(), Ytdlp); err == nil {
		t.Error("非托管模式下 Install 应当报错")
	}
}

func TestParseVersion(t *testing.T) {
	cases := []struct {
		kind Kind
		out  string
		want string
	}{
		{Ytdlp, "2026.01.15\n", "2026.01.15"},
		{FFmpeg, "ffmpeg version n7.1-31-g8b3f1d0 Copyright (c) 2000-2025\n", "n7.1-31-g8b3f1d0"},
		{FFmpeg, "ffmpeg version 6.1.1 Copyright (c) 2000-2023 the FFmpeg developers\n", "6.1.1"},
		// 意外的输出不能让整个状态查询崩掉
		{FFmpeg, "", ""},
		{Ytdlp, "", ""},
	}
	for _, c := range cases {
		if got := parseVersion(c.kind, c.out); got != c.want {
			t.Errorf("parseVersion(%s, %q) = %q, want %q", c.kind, c.out, got, c.want)
		}
	}
}

// 撞到大小上限时 io.Copy 是干净返回的(LimitReader 读完了),不显式
// 检查就会留下一个「能启动但缺后半截」的二进制,而且 EnsureBundled
// 还不会覆盖它——用户只能 SSH 进去手删。
func TestWriteExecutableRejectsOversize(t *testing.T) {
	old := maxDownload
	maxDownload = 1 << 10
	t.Cleanup(func() { maxDownload = old })

	dst := filepath.Join(t.TempDir(), "yt-dlp")
	err := writeExecutable(dst, strings.NewReader(strings.Repeat("x", 4<<10)))
	if err == nil {
		t.Fatal("超过上限应当报错")
	}
	if _, err := os.Stat(dst); err == nil {
		t.Error("报错后不该留下目标文件")
	}
	if _, err := os.Stat(dst + ".tmp"); err == nil {
		t.Error("报错后不该留下临时文件")
	}
}

func TestWriteExecutableRejectsEmpty(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "yt-dlp")
	if err := writeExecutable(dst, strings.NewReader("")); err == nil {
		t.Fatal("空内容应当报错")
	}
	if _, err := os.Stat(dst); err == nil {
		t.Error("报错后不该留下目标文件")
	}
}

func TestFindAsset(t *testing.T) {
	rel := &ghRelease{Assets: []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	}{
		{Name: "yt-dlp_musllinux", URL: "https://example.test/x86"},
		{Name: "yt-dlp_musllinux_aarch64", URL: "https://example.test/arm"},
	}}

	got, err := findAsset(rel, []string{"yt-dlp_musllinux_aarch64"})
	if err != nil {
		t.Fatalf("findAsset: %v", err)
	}
	if got != "https://example.test/arm" {
		t.Errorf("URL = %q", got)
	}

	if _, err := findAsset(rel, []string{"不存在的产物"}); err == nil {
		t.Error("找不到匹配产物时应当报错")
	}
}
