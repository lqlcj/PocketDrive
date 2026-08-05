package ytdlp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pocketdrive/internal/db"
)

func TestNormalizeCookiesAddsHeader(t *testing.T) {
	raw := ".youtube.com\tTRUE\t/\tTRUE\t1799999999\tSID\tabc123"
	out, err := normalizeCookies(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, netscapeHeader) {
		t.Fatalf("缺少 Netscape 头: %q", out)
	}
	if !strings.Contains(out, "SID\tabc123") {
		t.Fatalf("内容被改坏了: %q", out)
	}
}

func TestNormalizeCookiesKeepsExistingHeader(t *testing.T) {
	raw := netscapeHeader + "\n.youtube.com\tTRUE\t/\tTRUE\t1799999999\tSID\tabc"
	out, err := normalizeCookies(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, netscapeHeader) != 1 {
		t.Fatalf("头被加了两遍: %q", out)
	}
}

func TestNormalizeCookiesRejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "   ", "随便复制的一段网页文字", "a,b,c,d,e,f,g"} {
		if _, err := normalizeCookies(s); err == nil {
			t.Fatalf("应当拒绝: %q", s)
		}
	}
}

func TestInspectCookiesRecognizesModernAuthAndExactDomains(t *testing.T) {
	now := time.Unix(1700000000, 0)
	raw := strings.Join([]string{
		".youtube.com\tTRUE\t/\tTRUE\t1800000000\t__Secure-3PSID\tvalue",
		"notyoutube.com\tTRUE\t/\tTRUE\t1800000000\tSID\twrong-domain",
	}, "\n")
	status := inspectCookies(raw, now)
	if !status.Valid || status.CookieCount != 1 || status.AuthCount != 1 {
		t.Fatalf("现代 cookies 或域名判断错误: %+v", status)
	}
}

func TestValidateSettings(t *testing.T) {
	ok := []Settings{
		{},
		{Proxy: "socks5://127.0.0.1:1080"},
		{Proxy: "http://10.0.0.2:8080", PlayerClient: "tv"},
		{PlayerClient: "web_safari"},
	}
	for _, s := range ok {
		if err := validateSettings(&s); err != nil {
			t.Fatalf("%+v 应当通过: %v", s, err)
		}
	}
	bad := []Settings{
		{Proxy: "127.0.0.1:1080"},          // 没有 scheme
		{Proxy: "ftp://127.0.0.1:21"},      // 不支持的 scheme
		{PlayerClient: "; rm -rf /"},       // 不在白名单
		{PlayerClient: "web_safari; evil"}, // 同上
	}
	for _, s := range bad {
		if err := validateSettings(&s); err == nil {
			t.Fatalf("%+v 应当被拒", s)
		}
	}
}

// yt-dlp 会把刷新后的 cookie 写回 --cookies 指向的文件,所以传给它的
// 必须是副本,用户上传的原件不能动。
func TestRunCookiesUsesCopy(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{confDir: dir}
	if got := m.runCookies(); got != "" {
		t.Fatalf("没配 cookies 时应当返回空串,得到 %q", got)
	}
	orig := filepath.Join(dir, cookieFile)
	if err := os.WriteFile(orig, []byte("# Netscape HTTP Cookie File\nx\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := m.runCookies()
	if got == "" || got == orig {
		t.Fatalf("应当返回一份副本的路径,得到 %q", got)
	}
	// 模拟 yt-dlp 改写副本
	if err := os.WriteFile(got, []byte("轮换过了"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(orig)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "# Netscape HTTP Cookie File\nx\n" {
		t.Fatalf("原件被改了: %q", b)
	}
}

func TestNetworkArgs(t *testing.T) {
	dir := t.TempDir()
	gdb, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	// Windows: TempDir 清理前必须关掉 SQLite 连接,否则文件被占用
	t.Cleanup(func() {
		if sqlDB, err := gdb.DB(); err == nil {
			sqlDB.Close()
		}
	})
	m := &Manager{db: gdb, confDir: dir}

	if got := m.networkArgs(); len(got) != 0 {
		t.Fatalf("默认什么都不该加,得到 %v", got)
	}

	if err := m.saveSettings(Settings{
		Proxy: "socks5://127.0.0.1:1080", PlayerClient: "tv",
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, cookieFile),
		[]byte(netscapeHeader+"\nx\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := strings.Join(m.networkArgs(), " ")
	for _, want := range []string{
		"--proxy socks5://127.0.0.1:1080",
		"--cookies",
		"--extractor-args youtube:player_client=tv",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("缺少 %q,实际是 %q", want, got)
		}
	}
}
