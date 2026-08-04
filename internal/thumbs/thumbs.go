// Package thumbs generates and caches thumbnails: images are decoded
// and downscaled in pure Go; video covers are extracted with ffmpeg
// when available (404 otherwise, the frontend falls back to icons).
package thumbs

import (
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
	maxSide     = 320
	maxImageMB  = 30
	maxPixels   = 40_000_000 // 40MP 解码内存保护
	ffmpegOffet = "1"        // 视频抽第 1 秒的帧
)

var imageExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
}

var videoExts = map[string]bool{
	".mp4": true, ".webm": true, ".mkv": true, ".mov": true, ".avi": true,
	".m4v": true, ".flv": true,
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
		s.sem <- struct{}{}
		err = s.generate(p, fi.Size(), cachePath)
		<-s.sem
		if err != nil {
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

func (s *Service) generate(p string, size int64, cachePath string) error {
	ext := strings.ToLower(filepath.Ext(p))
	switch {
	case imageExts[ext]:
		return s.generateImage(p, size, cachePath)
	case videoExts[ext]:
		return s.generateVideo(p, cachePath)
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

func (s *Service) generateVideo(p, cachePath string) error {
	ff := s.ffmpeg()
	if ff == "" {
		return errors.New("未安装 ffmpeg,无法生成视频封面")
	}
	abs := filepath.Join(s.files.DataDir, filepath.FromSlash(p))
	tmp := cachePath + ".tmp.jpg"
	// exec 直调不经 shell;-ss 在 -i 前是快速 seek
	cmd := exec.Command(ff, "-y", "-loglevel", "error",
		"-ss", ffmpegOffet, "-i", abs,
		"-frames:v", "1", "-vf", fmt.Sprintf("scale=%d:-2", maxSide),
		tmp)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("ffmpeg 抽帧失败: %s", strings.TrimSpace(string(out)))
	}
	return os.Rename(tmp, cachePath)
}
