package auth

// 自定义头像:用户上传一张图,服务端解码、裁成正方形、缩到 256px 存成
// PNG。文件放在配置目录(与数据库同级),**不在网盘目录里**——否则它会
// 出现在文件列表和 WebDAV 里,还会被整盘打包带走。
//
// 没有上传过头像时不返回任何图片,前端用用户名首字母渲染,不占存储。

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	// x/image/draw 重导出了 image/draw 的 Op,同时提供高质量缩放
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"pocketdrive/internal/httpx"
)

const (
	avatarSide    = 256
	avatarMaxUp   = 8 << 20 // 上传原图上限 8MB
	avatarSetting = "avatar_version"
)

// avatarPath 是头像文件的落盘位置(配置目录下)。
func (s *Service) avatarPath() string {
	if s.configDir == "" {
		return ""
	}
	return filepath.Join(s.configDir, "avatar.png")
}

// HasAvatar 报告用户是否上传过头像。
func (s *Service) HasAvatar() bool {
	p := s.avatarPath()
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

// AvatarVersion 是头像的版本号(上传时间戳),用于给 URL 带上参数,
// 换了头像后浏览器不会继续用旧缓存。
func (s *Service) AvatarVersion() string {
	v, _ := s.getSetting(avatarSetting)
	return v
}

// HandleAvatar 返回头像图片;没上传过则 404,前端回退到首字母。
func (s *Service) HandleAvatar(w http.ResponseWriter, r *http.Request) {
	p := s.avatarPath()
	if p == "" {
		httpx.Err(w, http.StatusNotFound, "未设置头像")
		return
	}
	f, err := os.Open(p)
	if err != nil {
		httpx.Err(w, http.StatusNotFound, "未设置头像")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		httpx.Err(w, http.StatusNotFound, "未设置头像")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	// 带版本参数访问时可以长缓存;不带则每次校验
	if r.URL.Query().Get("v") != "" {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, "avatar.png", fi.ModTime(), f)
}

// HandleAvatarUpload 接收一张图片,裁成正方形缩到 256px 存起来。
func (s *Service) HandleAvatarUpload(w http.ResponseWriter, r *http.Request) {
	if s.configDir == "" {
		httpx.Err(w, http.StatusInternalServerError, "服务未配置头像存储目录")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, avatarMaxUp+1))
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "读取失败")
		return
	}
	if len(raw) > avatarMaxUp {
		httpx.Err(w, http.StatusRequestEntityTooLarge, "图片不能超过 8MB")
		return
	}
	img, err := decodeSquare(raw)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}

	// 先写临时文件再改名,避免写一半被读到
	p := s.avatarPath()
	tmp := p + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		os.Remove(tmp)
		httpx.Err(w, http.StatusInternalServerError, "编码失败: "+err.Error())
		return
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		httpx.Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		httpx.Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	version := strconv.FormatInt(time.Now().Unix(), 10)
	_ = s.setSetting(avatarSetting, version)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "version": version})
}

// HandleAvatarDelete 删掉自定义头像,回到首字母显示。
func (s *Service) HandleAvatarDelete(w http.ResponseWriter, r *http.Request) {
	if p := s.avatarPath(); p != "" {
		_ = os.Remove(p)
	}
	_ = s.setSetting(avatarSetting, "")
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// decodeSquare 解码图片,居中裁成正方形,再缩放到 avatarSide。
func decodeSquare(raw []byte) (image.Image, error) {
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("无法识别的图片格式,支持 PNG / JPEG / GIF / WebP")
	}
	b := src.Bounds()
	side := b.Dx()
	if b.Dy() < side {
		side = b.Dy()
	}
	if side <= 0 {
		return nil, errors.New("图片尺寸无效")
	}
	// 居中裁剪
	crop := image.Rect(
		b.Min.X+(b.Dx()-side)/2,
		b.Min.Y+(b.Dy()-side)/2,
		b.Min.X+(b.Dx()-side)/2+side,
		b.Min.Y+(b.Dy()-side)/2+side,
	)
	out := image.NewRGBA(image.Rect(0, 0, avatarSide, avatarSide))
	draw.CatmullRom.Scale(out, out.Bounds(), src, crop, draw.Over, nil)
	return out, nil
}
