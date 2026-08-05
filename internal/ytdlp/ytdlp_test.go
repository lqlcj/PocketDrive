package ytdlp

import (
	"strings"
	"testing"
)

func TestProgressRegex(t *testing.T) {
	m := rePercent.FindStringSubmatch("[download]  42.3% of ~ 123.45MiB at 1.23MiB/s ETA 00:12")
	if m == nil || m[1] != "42.3" {
		t.Fatalf("percent parse failed: %v", m)
	}
}

func TestDestRegex(t *testing.T) {
	m := reDest.FindStringSubmatch(`[download] Destination: /data/videos/我的视频.f137.mp4`)
	if m == nil || m[1] != "/data/videos/我的视频.f137.mp4" {
		t.Fatalf("dest parse failed: %v", m)
	}
	m = reDest.FindStringSubmatch(`[Merger] Merging formats into "/data/videos/我的视频.mp4"`)
	if m == nil || m[1] != "/data/videos/我的视频.mp4" {
		t.Fatalf("merger parse failed: %v", m)
	}
}

func TestBaseArgsEnableNodeAndKeepWarnings(t *testing.T) {
	got := strings.Join(baseArgs(), " ")
	if !strings.Contains(got, "--js-runtimes node") {
		t.Fatalf("没有启用 Node.js runtime: %q", got)
	}
	if strings.Contains(got, "--no-warnings") {
		t.Fatalf("不应隐藏 yt-dlp 的关键诊断: %q", got)
	}
}

func TestHintForDistinguishesCookieState(t *testing.T) {
	log := "ERROR: Sign in to confirm you’re not a bot"
	if got := hintFor(log, true, false); !strings.Contains(got, "没有检测到未过期") {
		t.Fatalf("无效 cookies 提示不正确: %q", got)
	}
	if got := hintFor(log, true, true); !strings.Contains(got, "已经把 cookies 传给") {
		t.Fatalf("有效 cookies 提示不正确: %q", got)
	}
	if got := hintFor("WARNING: No supported JavaScript runtime could be found", false, false); !strings.Contains(got, "整体镜像") {
		t.Fatalf("JS runtime 提示不正确: %q", got)
	}
}
