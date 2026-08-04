// Package share implements public share links for files: optional
// password (bcrypt) and optional expiry. Public endpoints are
// unauthenticated and rate-limited per IP on password attempts.
package share

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"pocketdrive/internal/cloud"
	"pocketdrive/internal/db"
	"pocketdrive/internal/files"
	"pocketdrive/internal/httpx"
	"pocketdrive/internal/thumbs"
)

type Service struct {
	db     *gorm.DB
	files  *files.Service
	thumbs *thumbs.Service
	cloud  *cloud.Service

	mu      sync.Mutex
	limiter map[string]*ipEntry
}

type ipEntry struct {
	fails        int
	blockedUntil time.Time
}

func New(gdb *gorm.DB, fs *files.Service, th *thumbs.Service, cs *cloud.Service) *Service {
	return &Service{db: gdb, files: fs, thumbs: th, cloud: cs, limiter: make(map[string]*ipEntry)}
}

func randToken(n int) string {
	const chars = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

func ctxTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// mountStat 解析分享路径是否指向外部存储;是则返回其挂载与文件信息。
func (s *Service) mountStat(ctx context.Context, p string) (*cloud.S3Mount, string, *cloud.Entry, error) {
	if !cloud.IsMountPath(p) {
		return nil, "", nil, nil
	}
	m, rel, ok := s.cloud.Resolve(p)
	if !ok || rel == "" {
		return nil, "", nil, errors.New("文件已被删除")
	}
	e, err := m.Stat(ctx, rel)
	if err != nil || e.Dir {
		return nil, "", nil, errors.New("文件已被删除")
	}
	return m, rel, &e, nil
}

func (s *Service) Create(p, password, shareType string, expiresHours int) (*db.Share, error) {
	p = files.CleanPath(p)
	if p == "" {
		return nil, errors.New("不能分享根目录")
	}
	if shareType != "direct" {
		shareType = "page"
	}
	if cloud.IsMountPath(p) {
		m, rel, ok := s.cloud.Resolve(p)
		if !ok {
			return nil, errors.New("外部存储不存在或未挂载")
		}
		if rel == "" {
			return nil, errors.New("不能分享挂载点本身")
		}
		ctx, cancel := ctxTimeout()
		defer cancel()
		e, err := m.Stat(ctx, rel)
		if err != nil {
			return nil, errors.New("文件不存在")
		}
		if e.Dir {
			return nil, errors.New("暂不支持分享文件夹,请分享单个文件")
		}
	} else {
		fi, err := s.files.Root().Stat(p)
		if err != nil {
			return nil, errors.New("文件不存在")
		}
		if fi.IsDir() {
			return nil, errors.New("暂不支持分享文件夹,请分享单个文件")
		}
	}
	sh := db.Share{Token: randToken(10), Path: p, Type: shareType}
	// 直链是给播放器/下载工具直接用的,不支持密码
	if password != "" && shareType == "page" {
		hb, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		sh.PasswordHash = string(hb)
		sh.HasPassword = true
	}
	if expiresHours > 0 {
		t := time.Now().Add(time.Duration(expiresHours) * time.Hour)
		sh.ExpiresAt = &t
	}
	if err := s.db.Create(&sh).Error; err != nil {
		return nil, err
	}
	return &sh, nil
}

func (s *Service) expired(sh *db.Share) bool {
	return sh.ExpiresAt != nil && time.Now().After(*sh.ExpiresAt)
}

func (s *Service) find(token string) (*db.Share, error) {
	var sh db.Share
	if err := s.db.First(&sh, "token = ?", token).Error; err != nil {
		return nil, errors.New("分享不存在或已删除")
	}
	if s.expired(&sh) {
		return nil, errors.New("分享已过期")
	}
	return &sh, nil
}

// ---- rate limiting for password attempts ----

func (s *Service) blocked(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.limiter[ip]
	return e != nil && time.Now().Before(e.blockedUntil)
}

func (s *Service) fail(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.limiter[ip]
	if e == nil {
		e = &ipEntry{}
		s.limiter[ip] = e
	}
	e.fails++
	if e.fails >= 5 {
		e.blockedUntil = time.Now().Add(5 * time.Minute)
		e.fails = 0
	}
}

func (s *Service) checkPassword(sh *db.Share, password, ip string) error {
	if !sh.HasPassword {
		return nil
	}
	if s.blocked(ip) {
		return errors.New("尝试次数过多,请 5 分钟后再试")
	}
	if bcrypt.CompareHashAndPassword([]byte(sh.PasswordHash), []byte(password)) != nil {
		s.fail(ip)
		return errors.New("提取密码不正确")
	}
	return nil
}

// ---- authenticated handlers ----

func (s *Service) HandleList(w http.ResponseWriter, r *http.Request) {
	var shares []db.Share
	s.db.Order("created_at DESC").Find(&shares)
	httpx.JSON(w, http.StatusOK, map[string]any{"shares": shares})
}

func (s *Service) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path         string `json:"path"`
		Password     string `json:"password"`
		Type         string `json:"type"`
		ExpiresHours int    `json:"expiresHours"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	sh, err := s.Create(req.Path, req.Password, req.Type, req.ExpiresHours)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "share": sh})
}

func (s *Service) HandleDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID uint `json:"id"`
	}
	if err := httpx.Decode(r, &req); err != nil || req.ID == 0 {
		httpx.Err(w, http.StatusBadRequest, "缺少 id")
		return
	}
	s.db.Delete(&db.Share{}, req.ID)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- public handlers (no auth) ----

func (s *Service) HandleInfo(w http.ResponseWriter, r *http.Request) {
	sh, err := s.find(r.PathValue("token"))
	if err != nil {
		httpx.Err(w, http.StatusNotFound, err.Error())
		return
	}
	if m, _, e, merr := s.mountStat(r.Context(), sh.Path); m != nil || merr != nil {
		if merr != nil {
			httpx.Err(w, http.StatusNotFound, merr.Error())
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{
			"name":        e.Name,
			"size":        e.Size,
			"mtime":       e.Mtime,
			"needPassword": sh.HasPassword,
			"expiresAt":   sh.ExpiresAt,
		})
		return
	}
	fi, err := s.files.Root().Stat(sh.Path)
	if err != nil {
		httpx.Err(w, http.StatusNotFound, "文件已被删除")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"name":        fi.Name(),
		"size":        fi.Size(),
		"mtime":       fi.ModTime().UnixMilli(),
		"needPassword": sh.HasPassword,
		"expiresAt":   sh.ExpiresAt,
	})
}

func (s *Service) HandleDownload(w http.ResponseWriter, r *http.Request) {
	sh, err := s.find(r.PathValue("token"))
	if err != nil {
		httpx.Err(w, http.StatusNotFound, err.Error())
		return
	}
	if err := s.checkPassword(sh, r.URL.Query().Get("password"), httpx.ClientIP(r)); err != nil {
		httpx.Err(w, http.StatusForbidden, err.Error())
		return
	}
	if m, rel, e, merr := s.mountStat(r.Context(), sh.Path); m != nil || merr != nil {
		if merr != nil {
			httpx.Err(w, http.StatusNotFound, merr.Error())
			return
		}
		u, perr := m.PresignGet(r.Context(), rel, e.Name, r.URL.Query().Get("dl") == "1")
		if perr != nil {
			httpx.Err(w, http.StatusBadGateway, "生成下载链接失败: "+perr.Error())
			return
		}
		http.Redirect(w, r, u, http.StatusFound)
		return
	}
	f, err := s.files.Root().Open(sh.Path)
	if err != nil {
		httpx.Err(w, http.StatusNotFound, "文件已被删除")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		httpx.Err(w, http.StatusNotFound, "文件不可用")
		return
	}
	if r.URL.Query().Get("dl") == "1" {
		w.Header().Set("Content-Disposition",
			"attachment; filename*=UTF-8''"+url.PathEscape(fi.Name()))
	}
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}

// HandleThumb serves a share's media thumbnail (public; password
// checked the same way as download). 外部存储不生成缩略图。
func (s *Service) HandleThumb(w http.ResponseWriter, r *http.Request) {
	sh, err := s.find(r.PathValue("token"))
	if err != nil {
		httpx.Err(w, http.StatusNotFound, err.Error())
		return
	}
	if err := s.checkPassword(sh, r.URL.Query().Get("password"), httpx.ClientIP(r)); err != nil {
		httpx.Err(w, http.StatusForbidden, err.Error())
		return
	}
	if cloud.IsMountPath(sh.Path) {
		httpx.Err(w, http.StatusNotFound, "外部存储不提供缩略图")
		return
	}
	s.thumbs.Serve(w, r, sh.Path)
}

// HandleDirect serves /d/{token} and /d/{token}/{name}: raw file
// stream for direct-link shares — usable by players and download
// tools, no password. The optional {name} segment only makes the URL
// carry a real filename + extension (players and download tools pick
// file type by suffix); the token alone is the credential.
func (s *Service) HandleDirect(w http.ResponseWriter, r *http.Request) {
	sh, err := s.find(r.PathValue("token"))
	if err != nil || sh.Type != "direct" {
		httpx.Err(w, http.StatusNotFound, "直链不存在或已过期")
		return
	}
	if m, rel, e, merr := s.mountStat(r.Context(), sh.Path); m != nil || merr != nil {
		if merr != nil {
			httpx.Err(w, http.StatusNotFound, merr.Error())
			return
		}
		u, perr := m.PresignGet(r.Context(), rel, e.Name, false)
		if perr != nil {
			httpx.Err(w, http.StatusBadGateway, "生成下载链接失败: "+perr.Error())
			return
		}
		http.Redirect(w, r, u, http.StatusFound)
		return
	}
	f, err := s.files.Root().Open(sh.Path)
	if err != nil {
		httpx.Err(w, http.StatusNotFound, "文件已被删除")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		httpx.Err(w, http.StatusNotFound, "文件不可用")
		return
	}
	// inline:浏览器/播放器直接打开;filename 让 wget/IDM 等保存时
	// 用真实文件名(即使拿到的是不带文件名段的旧格式链接)
	w.Header().Set("Content-Disposition",
		"inline; filename*=UTF-8''"+url.PathEscape(fi.Name()))
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}

// PathBase is a small helper for the frontend share dialog.
func PathBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
