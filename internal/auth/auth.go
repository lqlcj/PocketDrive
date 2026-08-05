package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"pocketdrive/internal/db"
	"pocketdrive/internal/httpx"
)

const (
	cookieName  = "pocketdrive_token"
	tokenTTL    = 7 * 24 * time.Hour
	maxFails    = 5
	blockPeriod = 5 * time.Minute
)

type ipEntry struct {
	fails        int
	blockedUntil time.Time
}

type Service struct {
	db            *gorm.DB
	user          string
	webdavEnabled bool
	webdavUser    string
	webdavHash    string
	secret        []byte
	tokenVersion  int64
	// configDir 存放自定义头像。刻意放在网盘目录之外:否则头像会出现
	// 在文件列表和 WebDAV 里,还会被整盘备份带走
	configDir string

	mu sync.Mutex
	// 成功过的 Basic 凭据摘要缓存:WebDAV 客户端每个请求都带 Basic,
	// 不缓存的话每次 bcrypt 校验 ~100ms,手机播放器会卡
	passCache map[[32]byte]struct{}
	limiter   map[string]*ipEntry
}

func New(gdb *gorm.DB, user, initialPass, configDir string) (*Service, error) {
	s := &Service{
		db:        gdb,
		user:      user,
		configDir: configDir,
		passCache: make(map[[32]byte]struct{}),
		limiter:   make(map[string]*ipEntry),
	}

	sec, err := s.getSetting("jwt_secret")
	if err != nil {
		return nil, err
	}
	if sec == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		sec = hex.EncodeToString(b)
		if err := s.setSetting("jwt_secret", sec); err != nil {
			return nil, err
		}
	}
	s.secret, err = hex.DecodeString(sec)
	if err != nil {
		return nil, err
	}

	h, err := s.getSetting("admin_hash")
	if err != nil {
		return nil, err
	}
	if h == "" {
		pass := initialPass
		generated := false
		if pass == "" {
			pass = randPassword(12)
			generated = true
		}
		hb, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		if err := s.setSetting("admin_hash", string(hb)); err != nil {
			return nil, err
		}
		if generated {
			log.Printf("已生成初始管理员密码: %s (用户 %q) — 请登录后在设置页修改", pass, user)
		}
	}

	// 用户名持久化:首次启动写入 env 提供的值,之后以 DB 为准(设置页可改)
	storedUser, err := s.getSetting("admin_user")
	if err != nil {
		return nil, err
	}
	if storedUser == "" {
		if err := s.setSetting("admin_user", user); err != nil {
			return nil, err
		}
	} else {
		s.user = storedUser
	}
	s.webdavEnabled = s.getSettingOr("webdav_enabled", "true") != "false"
	if _, err := fmt.Sscanf(s.getSettingOr("token_version", "1"), "%d", &s.tokenVersion); err != nil || s.tokenVersion < 1 {
		s.tokenVersion = 1
	}
	if err := s.setSetting("token_version", fmt.Sprintf("%d", s.tokenVersion)); err != nil {
		return nil, err
	}
	s.webdavUser = s.getSettingOr("webdav_user", s.user)
	s.webdavHash = s.getSettingOr("webdav_hash", "")
	return s, nil
}

func (s *Service) getSettingOr(key, fallback string) string {
	v, err := s.getSetting(key)
	if err != nil || v == "" {
		return fallback
	}
	return v
}

type WebDAVSettings struct {
	Enabled bool   `json:"enabled"`
	User    string `json:"user"`
}

func (s *Service) WebDAVSettings() WebDAVSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.webdavUser
	if u == "" {
		u = s.user
	}
	return WebDAVSettings{Enabled: s.webdavEnabled, User: u}
}
func (s *Service) HandleGetWebDAVSettings(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{"settings": s.WebDAVSettings()})
}
func (s *Service) HandleSaveWebDAVSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Enabled  *bool  `json:"enabled"`
		User     string `json:"user"`
		Password string `json:"password"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, 400, "请求格式错误")
		return
	}
	u := strings.TrimSpace(req.User)
	if u != "" && (len(u) < 2 || len(u) > 32 || strings.ContainsAny(u, ": \t\r\n")) {
		httpx.Err(w, 400, "用户名 2-32 字符,不能含冒号和空白")
		return
	}
	s.mu.Lock()
	if req.Enabled != nil {
		s.webdavEnabled = *req.Enabled
	}
	if u != "" {
		s.webdavUser = u
	}
	s.mu.Unlock()
	if req.Password != "" {
		if len(req.Password) < 6 {
			httpx.Err(w, 400, "密码至少 6 位")
			return
		}
		h, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			httpx.Err(w, 500, err.Error())
			return
		}
		s.mu.Lock()
		s.webdavHash = string(h)
		s.mu.Unlock()
	}
	s.setSetting("webdav_enabled", fmt.Sprintf("%t", s.webdavEnabled))
	s.setSetting("webdav_user", s.webdavUser)
	if s.webdavHash != "" {
		s.setSetting("webdav_hash", s.webdavHash)
	}
	httpx.JSON(w, 200, map[string]any{"ok": true, "settings": s.WebDAVSettings()})
}

func (s *Service) User() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.user
}

func (s *Service) Avatar() string {
	v, _ := s.getSetting("avatar")
	if v == "" {
		v = "🏝️"
	}
	return v
}

func (s *Service) getSetting(key string) (string, error) {
	var st db.Setting
	err := s.db.First(&st, "key = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	return st.Value, err
}

func (s *Service) setSetting(key, value string) error {
	return s.db.Save(&db.Setting{Key: key, Value: value}).Error
}

func randPassword(n int) string {
	const chars = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ23456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	for i := range b {
		b[i] = chars[int(b[i])%len(chars)]
	}
	return string(b)
}

// Check verifies username/password against the stored bcrypt hash,
// with a digest cache so repeated WebDAV requests skip bcrypt.
func (s *Service) Check(user, pass string) bool {
	if user != s.User() {
		return false
	}
	key := sha256.Sum256([]byte(user + "\x00" + pass))
	s.mu.Lock()
	_, cached := s.passCache[key]
	s.mu.Unlock()
	if cached {
		return true
	}
	h, err := s.getSetting("admin_hash")
	if err != nil || h == "" {
		return false
	}
	if bcrypt.CompareHashAndPassword([]byte(h), []byte(pass)) != nil {
		return false
	}
	s.mu.Lock()
	s.passCache[key] = struct{}{}
	s.mu.Unlock()
	return true
}

func (s *Service) ChangePassword(oldPass, newPass string) error {
	if len(newPass) < 6 {
		return errors.New("新密码至少 6 位")
	}
	if !s.Check(s.User(), oldPass) {
		return errors.New("旧密码不正确")
	}
	hb, err := bcrypt.GenerateFromPassword([]byte(newPass), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	s.mu.Lock()
	nextVersion := s.tokenVersion + 1
	s.mu.Unlock()
	// Persist invalidation first. If the password write fails, existing
	// sessions are still revoked rather than becoming valid again after restart.
	if err := s.setSetting("token_version", fmt.Sprintf("%d", nextVersion)); err != nil {
		return err
	}
	if err := s.setSetting("admin_hash", string(hb)); err != nil {
		return err
	}
	s.mu.Lock()
	s.passCache = make(map[[32]byte]struct{})
	s.tokenVersion = nextVersion
	s.mu.Unlock()
	return nil
}

func (s *Service) issueToken() (string, time.Time, error) {
	exp := time.Now().Add(tokenTTL)
	s.mu.Lock()
	version := s.tokenVersion
	s.mu.Unlock()
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": s.User(),
		"exp": exp.Unix(),
		"ver": version,
	})
	str, err := t.SignedString(s.secret)
	return str, exp, err
}

func (s *Service) verifyToken(tok string) bool {
	t, err := jwt.Parse(tok, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	})
	if err != nil || !t.Valid {
		return false
	}
	claims, ok := t.Claims.(jwt.MapClaims)
	if !ok || claims["sub"] != s.User() {
		return false
	}
	ver, ok := claims["ver"].(float64)
	s.mu.Lock()
	currentVersion := s.tokenVersion
	s.mu.Unlock()
	return ok && int64(ver) == currentVersion
}

// ---- rate limiting ----

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
	if e.fails >= maxFails {
		e.blockedUntil = time.Now().Add(blockPeriod)
		e.fails = 0
	}
}

func (s *Service) succeed(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.limiter, ip)
}

// ---- middleware ----

func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := ""
		if c, err := r.Cookie(cookieName); err == nil {
			tok = c.Value
		}
		if ah := r.Header.Get("Authorization"); strings.HasPrefix(ah, "Bearer ") {
			tok = strings.TrimPrefix(ah, "Bearer ")
		}
		if tok == "" || !s.verifyToken(tok) {
			httpx.Err(w, http.StatusUnauthorized, "未登录或登录已过期")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// BasicAuth protects WebDAV with the same admin account.
func (s *Service) BasicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := httpx.ClientIP(r)
		if s.blocked(ip) {
			http.Error(w, "too many failed attempts, try later", http.StatusTooManyRequests)
			return
		}
		user, pass, ok := r.BasicAuth()
		s.mu.Lock()
		enabled, davUser, davHash := s.webdavEnabled, s.webdavUser, s.webdavHash
		s.mu.Unlock()
		valid := enabled && ok
		if valid {
			if davHash != "" {
				valid = user == davUser && bcrypt.CompareHashAndPassword([]byte(davHash), []byte(pass)) == nil
			} else {
				valid = s.Check(user, pass)
			}
		}
		if !valid {
			if ok {
				s.fail(ip)
			}
			w.Header().Set("WWW-Authenticate", `Basic realm="PocketDrive", charset="UTF-8"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		s.succeed(ip)
		next.ServeHTTP(w, r)
	})
}

// CSRF rejects cross-site mutating requests. Cookie-authed browser
// traffic is same-origin (dev via Vite proxy); Bearer/Basic requests
// carry Authorization and are exempt.
func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if r.Header.Get("Authorization") != "" {
			next.ServeHTTP(w, r)
			return
		}
		if sfs := r.Header.Get("Sec-Fetch-Site"); sfs != "" {
			if sfs != "same-origin" && sfs != "none" {
				httpx.Err(w, http.StatusForbidden, "跨站请求被拒绝")
				return
			}
		} else if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				httpx.Err(w, http.StatusForbidden, "跨站请求被拒绝")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---- handlers ----

func (s *Service) HandleLogin(w http.ResponseWriter, r *http.Request) {
	ip := httpx.ClientIP(r)
	if s.blocked(ip) {
		httpx.Err(w, http.StatusTooManyRequests, "失败次数过多,请 5 分钟后再试")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if !s.Check(req.Username, req.Password) {
		s.fail(ip)
		httpx.Err(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	s.succeed(ip)
	tok, exp, err := s.issueToken()
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    tok,
		Path:     "/",
		Expires:  exp,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "user": s.User(), "avatar": s.Avatar(), "hasAvatar": s.HasAvatar(), "avatarVersion": s.AvatarVersion()})
}

func (s *Service) HandleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) HandleMe(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{"user": s.User(), "avatar": s.Avatar(), "hasAvatar": s.HasAvatar(), "avatarVersion": s.AvatarVersion()})
}

// HandleProfile updates username and/or avatar. Username change also
// applies to WebDAV Basic Auth immediately.
func (s *Service) HandleProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Avatar   string `json:"avatar"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.Username != "" {
		u := strings.TrimSpace(req.Username)
		if len(u) < 2 || len(u) > 32 || strings.ContainsAny(u, ": \t\r\n") {
			httpx.Err(w, http.StatusBadRequest, "用户名 2-32 字符,不能含冒号和空白")
			return
		}
		if err := s.setSetting("admin_user", u); err != nil {
			httpx.Err(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.mu.Lock()
		s.user = u
		s.passCache = make(map[[32]byte]struct{})
		s.mu.Unlock()
	}
	if req.Avatar != "" {
		if len(req.Avatar) > 16 {
			httpx.Err(w, http.StatusBadRequest, "头像格式不正确")
			return
		}
		if err := s.setSetting("avatar", req.Avatar); err != nil {
			httpx.Err(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "user": s.User(), "avatar": s.Avatar(), "hasAvatar": s.HasAvatar(), "avatarVersion": s.AvatarVersion()})
}

func (s *Service) HandleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := s.ChangePassword(req.OldPassword, req.NewPassword); err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}
