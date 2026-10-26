package thumbs

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"pocketdrive/internal/cloud"
	"pocketdrive/internal/db"
	"pocketdrive/internal/files"
)

func TestCameraVideoUsesThumbnailGenerator(t *testing.T) {
	svc := &Service{}
	for _, name := range []string{"camera.MTS", "camera.m2ts"} {
		err := svc.generate(context.Background(), name, 0, "unused.jpg")
		if err == nil || err.Error() == "此类型不支持缩略图" {
			t.Errorf("%s should reach video generation without ffmpeg, got %v", name, err)
		}
	}
}

func TestVideoArgumentsRestrictInput(t *testing.T) {
	args := videoArgs("thumb.jpg")
	for _, pair := range [][2]string{
		{"-protocol_whitelist", "fd"},
		{"-format_whitelist", "mov,matroska,webm,avi,flv,mpegts,m4v"},
		{"-i", "fd:"},
		{"-threads", "1"},
	} {
		i := slices.Index(args, pair[0])
		if i < 0 || i+1 >= len(args) || args[i+1] != pair[1] {
			t.Fatalf("missing %v in %v", pair, args)
		}
	}
	if !slices.Contains(args, "-nostdin") || args[len(args)-1] != "thumb.jpg" {
		t.Fatalf("unsafe arguments: %v", args)
	}
}

func TestFFmpegTimeoutStopsProcess(t *testing.T) {
	t.Setenv("POCKETDRIVE_THUMB_TEST_HELPER", "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := runFFmpeg(ctx, os.Args[0], nil, "-test.run=^TestFFmpegHelperProcess$")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("timed-out child process did not stop promptly")
	}
}

func TestFFmpegOutputIsBounded(t *testing.T) {
	t.Setenv("POCKETDRIVE_THUMB_TEST_HELPER", "large-error")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := runFFmpeg(ctx, os.Args[0], nil, "-test.run=^TestFFmpegHelperProcess$")
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(err.Error()) > maxFFmpegOutput+256 {
		t.Fatalf("unbounded error output: %d bytes", len(err.Error()))
	}
}

func TestFFmpegHelperProcess(t *testing.T) {
	switch os.Getenv("POCKETDRIVE_THUMB_TEST_HELPER") {
	case "hang":
		time.Sleep(time.Minute)
		os.Exit(0)
	case "large-error":
		_, _ = os.Stderr.Write(bytes.Repeat([]byte("x"), 1<<20))
		os.Exit(1)
	}
}

func newThumbnailTestService(t *testing.T, ff string) *Service {
	t.Helper()
	dir := t.TempDir()
	gdb, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := gdb.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	fs, err := files.New(filepath.Join(dir, "data"), filepath.Join(dir, "uploads"), cloud.New(gdb), gdb)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Root().Close() })
	return New(fs, filepath.Join(dir, "thumbs"), func() string { return ff })
}

func TestCancelledThumbnailDoesNotWaitForSlot(t *testing.T) {
	svc := newThumbnailTestService(t, "")
	f, err := svc.files.Root().Create("video.mp4")
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	svc.sem <- struct{}{}
	svc.sem <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/thumb", nil).WithContext(ctx)
	done := make(chan struct{})
	go func() { svc.Serve(w, r, "video.mp4"); close(done) }()
	select {
	case <-done:
		if w.Code != http.StatusRequestTimeout || len(svc.sem) != 2 {
			t.Fatalf("status=%d, occupied slots=%d", w.Code, len(svc.sem))
		}
	case <-time.After(2 * time.Second):
		<-svc.sem
		<-done
		t.Fatal("cancelled request blocked on a thumbnail slot")
	}
}

func TestVideoThumbnailConfinedInput(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg 6.1+ is required for the media integration test")
	}
	svc := newThumbnailTestService(t, ff)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	source := filepath.Join(svc.files.DataDir, "video.mp4")
	// No faststart: the metadata is at the end, so fd input must support seek.
	create := exec.CommandContext(ctx, ff, "-nostdin", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "color=c=blue:s=32x32:d=2", "-c:v", "mpeg4", source)
	if out, err := create.CombinedOutput(); err != nil {
		t.Fatalf("create video: %v: %s", err, out)
	}
	target := filepath.Join(svc.cacheDir, "valid.jpg")
	if err := svc.generateVideo(ctx, "video.mp4", target); err != nil {
		t.Fatalf("seekable MP4 thumbnail failed: %v", err)
	}
	if data, err := os.ReadFile(target); err != nil || len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		t.Fatalf("invalid JPEG output: %v", err)
	}
	var requests atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer remote.Close()
	playlist := "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:2\n#EXTINF:2,\n" + remote.URL + "/segment.ts\n#EXT-X-ENDLIST\n"
	if err := os.WriteFile(filepath.Join(svc.files.DataDir, "playlist.mp4"), []byte(playlist), 0600); err != nil {
		t.Fatal(err)
	}
	if err := svc.generateVideo(ctx, "playlist.mp4", filepath.Join(svc.cacheDir, "bad.jpg")); err == nil {
		t.Fatal("playlist disguised as a video was accepted")
	}
	if requests.Load() != 0 {
		t.Fatal("ffmpeg followed an external playlist URL")
	}
}
