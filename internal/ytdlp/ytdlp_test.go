package ytdlp

import "testing"

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
