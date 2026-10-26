// Package thumbs generates and caches thumbnails: images are decoded
// and downscaled in pure Go; video covers are extracted with ffmpeg
// when available (404 otherwise, the frontend falls back to icons).
package thumbs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"

	"pocketdrive/internal/files"
	"pocketdrive/internal/httpx"
)

const (
	maxSide          = 320
	maxImageMB       = 30
	maxPixels        = 40_000_000 // 40MP 解码内存保护
	ffmpegOffet      = "1"        // 视频抽第 1 秒的帧
	thumbnailTimeout = 30 * time.Second
	maxFFmpegOutput  = 16 << 10
)

var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
}

var videoExts = map[string]bool{
	".mp4": true, ".webm": true, ".mkv": true, ".mov": true, ".avi": true,
	".m4v": true, ".flv": true, ".mts": true, ".m2ts": true,
}

type Service struct {
	files    *files.Service
	cacheDir string
	sem      chan struct{}
	// ffmpegPath 每次调用时才解析:用户在网页里装好 ffmpeg 后立刻生效,
	// 不必重启服务
	ffmpegPath func() string
}

func New(fs *files.Service, cacheDir string, ffmpegPath func() string) *Service {
	_ = os.MkdirAll(cacheDir, 0o755)
	return &Service{
		files:      fs,
		cacheDir:   cacheDir,
		sem:        make(chan struct{}, 2),
		ffmpegPath: ffmpegPath,
	}
}

// ffmpeg 返回可用的 ffmpeg 路径,不可用时返回空串。
func (s *Service) ffmpeg() string {
	if s.ffmpegPath == nil {
		return ""
	}
	p := s.ffmpegPath()
	if p == "" {
		return ""
	}
	if _, err := exec.LookPath(p); err != nil {
		return ""
	}
	return p
}

func (s *Service) HandleThumb(w http.ResponseWriter, r *http.Request) {
	p := files.CleanPath(r.URL.Query().Get("path"))
	if p == "" {
		httpx.Err(w, http.StatusBadRequest, "路径不能为空")
		return
	}
	s.Serve(w, r, p)
}

// Serve renders/serves the cached thumbnail for an already-cleaned path.
func (s *Service) Serve(w http.ResponseWriter, r *http.Request, p string) {
	fi, err := s.files.Root().Stat(p)
	if err != nil || fi.IsDir() {
		httpx.Err(w, http.StatusNotFound, "文件不存在")
		return
	}
	key := cacheKey(p, fi.Size(), fi.ModTime())
	cachePath := filepath.Join(s.cacheDir, key+".jpg")
	if _, err := os.Stat(cachePath); err != nil {
		ctx, cancel := context.WithTimeout(r.Context(), thumbnailTimeout)
		defer cancel()
		select {
		case s.sem <- struct{}{}:
		case <-ctx.Done():
			httpx.Err(w, http.StatusRequestTimeout, "缩略图请求已取消或超时")
			return
		}
		err = func() error {
			defer func() { <-s.sem }()
			if err := ctx.Err(); err != nil {
				return err
			}
			// A previous request may have filled the cache while we waited.
			if _, err := os.Stat(cachePath); err == nil {
				return nil
			}
			return s.generate(ctx, p, fi.Size(), cachePath)
		}()
		if err != nil {
			if ctx.Err() != nil {
				httpx.Err(w, http.StatusRequestTimeout, "缩略图请求已取消或超时")
				return
			}
			httpx.Err(w, http.StatusNotFound, err.Error())
			return
		}
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, cachePath)
}

func cacheKey(p string, size int64, mtime time.Time) string {
	h := sha256.Sum256(fmt.Appendf(nil, "%s|%d|%d", p, size, mtime.UnixMilli()))
	return hex.EncodeToString(h[:8])
}

func (s *Service) generate(ctx context.Context, p string, size int64, cachePath string) error {
	ext := strings.ToLower(filepath.Ext(p))
	switch {
	case imageExts[ext]:
		return s.generateImage(p, size, cachePath)
	case videoExts[ext]:
		return s.generateVideo(ctx, p, cachePath)
	}
	return errors.New("此类型不支持缩略图")
}

func (s *Service) generateImage(p string, size int64, cachePath string) error {
	if size > maxImageMB<<20 {
		return errors.New("图片过大")
	}
	f, err := s.files.Root().Open(p)
	if err != nil {
		return err
	}
	defer f.Close()

	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return errors.New("无法解析图片")
	}
	if cfg.Width*cfg.Height > maxPixels {
		return errors.New("图片分辨率过大")
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	var img image.Image
	ext := strings.ToLower(filepath.Ext(p))
	switch ext {
	case ".webp":
		img, err = webp.Decode(f)
	case ".png":
		img, err = png.Decode(f)
	case ".gif":
		img, err = gif.Decode(f)
	default:
		img, err = jpeg.Decode(f)
	}
	if err != nil {
		return errors.New("图片解码失败")
	}

	b := img.Bounds()
	w, h := fitInto(b.Dx(), b.Dy(), maxSide)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return writeJPEG(dst, cachePath)
}

func fitInto(w, h, max int) (int, int) {
	if w <= max && h <= max {
		return w, h
	}
	if w >= h {
		return max, h * max / w
	}
	return w * max / h, max
}

func writeJPEG(img image.Image, cachePath string) error {
	tmp := cachePath + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := jpeg.Encode(out, img, &jpeg.Options{Quality: 78}); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	out.Close()
	return os.Rename(tmp, cachePath)
}

func (s *Service) generateVideo(ctx context.Context, p, cachePath string) error {
	ff := s.ffmpeg()
	if ff == "" {
		return errors.New("未安装 ffmpeg,无法生成视频封面")
	}
	// Pass the file already opened under os.Root, rather than letting ffmpeg
	// reopen a path that could be swapped for an escaping symlink.
	input, err := s.files.Root().Open(p)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("视频必须是普通文件")
	}
	output, err := os.CreateTemp(s.cacheDir, ".pd-thumb-*.jpg")
	if err != nil {
		return err
	}
	tmp := output.Name()
	defer os.Remove(tmp)
	if err := output.Close(); err != nil {
		return err
	}
	if err := runFFmpeg(ctx, ff, input, videoArgs(tmp)...); err != nil {
		return err
	}
	if info, err := os.Stat(tmp); err != nil {
		return err
	} else if info.Size() == 0 {
		return errors.New("视频没有可提取的画面")
	}
	return os.Rename(tmp, cachePath)
}

func videoArgs(target string) []string {
	// Local media only: do not auto-detect playlists that can fetch network
	// URLs or follow references into other local files. Unlike pipe:, fd:
	// preserves seeking for MP4 files with metadata at the end of the file.
	return []string{"-y", "-nostdin", "-loglevel", "error",
		"-protocol_whitelist", "fd",
		"-format_whitelist", "mov,matroska,webm,avi,flv,mpegts,m4v",
		"-threads", "1", "-ss", ffmpegOffet, "-i", "fd:",
		"-frames:v", "1", "-vf", fmt.Sprintf("scale=%d:-2", maxSide),
		"-threads", "1", target}
}

type limitedOutput struct {
	buffer bytes.Buffer
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := maxFFmpegOutput - b.buffer.Len(); remaining > 0 {
		_, _ = b.buffer.Write(p[:min(n, remaining)])
	}
	return n, nil
}

func runFFmpeg(ctx context.Context, ff string, input *os.File, args ...string) error {
	cmd := exec.CommandContext(ctx, ff, args...)
	cmd.WaitDelay = time.Second
	if input != nil {
		cmd.Stdin = input
	}
	var output limitedOutput
	cmd.Stderr = &output
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ffmpeg 抽帧失败: %w: %s", err, strings.TrimSpace(output.buffer.String()))
	}
	return ctx.Err()
}
