package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"gorm.io/gorm"

	"pocketdrive/internal/db"
	"pocketdrive/internal/httpx"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	gdb, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	s, err := New(gdb, "admin", "old-password", dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCredentialChangeRejectsInFlightPasswordCheck(t *testing.T) {
	for _, rename := range []bool{false, true} {
		name := "password"
		if rename {
			name = "rename-and-restore"
		}
		t.Run(name, func(t *testing.T) {
			s := newTestService(t)
			started, resume := make(chan struct{}), make(chan struct{})
			var held atomic.Bool
			var release sync.Once
			defer release.Do(func() { close(resume) })
			err := s.db.Callback().Query().After("gorm:query").Register("test:pause-old-hash", func(tx *gorm.DB) {
				setting, ok := tx.Statement.Dest.(*db.Setting)
				if ok && setting.Key == "admin_hash" && held.CompareAndSwap(false, true) {
					close(started)
					<-resume
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			checked := make(chan bool, 1)
			go func() { checked <- s.Check("admin", "old-password") }()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("password query did not start")
			}
			if rename {
				if err := s.changeUsername("renamed"); err != nil {
					t.Fatal(err)
				}
				if err := s.changeUsername("admin"); err != nil {
					t.Fatal(err)
				}
			} else if err := s.ChangePassword("old-password", "new-password"); err != nil {
				t.Fatal(err)
			}
			release.Do(func() { close(resume) })
			select {
			case valid := <-checked:
				if valid {
					t.Fatal("in-flight check survived a credential change")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("password check did not finish")
			}
			if !rename && (s.Check("admin", "old-password") || !s.Check("admin", "new-password")) {
				t.Fatal("password cache does not match the committed credentials")
			}
		})
	}
}

func TestPasswordRotationRevokesDelayedTokensAndSurvivesRestart(t *testing.T) {
	s := newTestService(t)
	version, valid := s.checkCredentials("admin", "old-password")
	if !valid {
		t.Fatal("initial credentials failed")
	}
	if err := s.ChangePassword("old-password", "new-password"); err != nil {
		t.Fatal(err)
	}
	// Signing after rotation must still use the generation checked at login.
	stale, _, err := s.issueToken("admin", version)
	if err != nil {
		t.Fatal(err)
	}
	if s.verifyToken(stale) {
		t.Fatal("old password check minted a usable post-rotation token")
	}
	restarted, err := New(s.db, "admin", "unused", s.configDir)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.verifyToken(stale) || restarted.Check("admin", "old-password") {
		t.Fatal("revoked credentials became valid after restart")
	}
	version, valid = restarted.checkCredentials("admin", "new-password")
	if !valid {
		t.Fatal("new password was not persisted")
	}
	fresh, _, err := restarted.issueToken("admin", version)
	if err != nil || !restarted.verifyToken(fresh) {
		t.Fatalf("new token rejected: %v", err)
	}
}

func TestPasswordRotationRollsBackOnDatabaseFailure(t *testing.T) {
	s := newTestService(t)
	version := s.tokenVersion
	if err := s.db.Callback().Update().Before("gorm:update").Register("test:fail-version-write", func(tx *gorm.DB) {
		if setting, ok := tx.Statement.Dest.(*db.Setting); ok && setting.Key == "token_version" {
			tx.AddError(errors.New("simulated database failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangePassword("old-password", "new-password"); err == nil {
		t.Fatal("expected a failed credential transaction")
	}
	if err := s.db.Callback().Update().Remove("test:fail-version-write"); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(s.db, "admin", "unused", s.configDir)
	if err != nil {
		t.Fatal(err)
	}
	if s.tokenVersion != version || restarted.tokenVersion != version ||
		!restarted.Check("admin", "old-password") || restarted.Check("admin", "new-password") {
		t.Fatal("failed transaction partially changed credentials")
	}
}

func TestSessionCookiesRespectHTTPSPolicy(t *testing.T) {
	s := newTestService(t)
	for _, tc := range []struct {
		name, scheme, forwarded string
		force, secure           bool
	}{
		{"http", "http", "", false, false},
		{"tls", "https", "", false, true},
		{"proxy", "http", "https", true, true},
		{"forged-header", "http", "https", false, false},
		{"tls-cannot-downgrade", "https", "http", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, logout := range []bool{false, true} {
				var body bytes.Buffer
				_ = json.NewEncoder(&body).Encode(map[string]string{"username": "admin", "password": "old-password"})
				r := httptest.NewRequest(http.MethodPost, tc.scheme+"://drive.example/api/v1/auth/login", &body)
				r.Header.Set("X-Forwarded-Proto", tc.forwarded)
				w := httptest.NewRecorder()
				h := http.HandlerFunc(s.HandleLogin)
				if logout {
					h = s.HandleLogout
				}
				httpx.WithSecureCookies(tc.force, h).ServeHTTP(w, r)
				cookies := w.Result().Cookies()
				if w.Code != http.StatusOK || len(cookies) != 1 {
					t.Fatalf("status=%d, cookies=%d", w.Code, len(cookies))
				}
				c := cookies[0]
				if c.Name != cookieName || c.Secure != tc.secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" {
					t.Errorf("incorrect cookie flags: %+v", c)
				}
				if logout && c.MaxAge != -1 {
					t.Fatal("logout did not expire the cookie")
				}
			}
		})
	}
}

func TestTokenRequiresExactAlgorithmExpiryAndVersion(t *testing.T) {
	s := newTestService(t)
	for _, kind := range []string{"valid", "wrong-algorithm", "no-expiry", "fractional-version"} {
		t.Run(kind, func(t *testing.T) {
			claims := jwt.MapClaims{"sub": "admin", "ver": s.tokenVersion, "exp": time.Now().Add(time.Hour).Unix()}
			var method jwt.SigningMethod = jwt.SigningMethodHS256
			switch kind {
			case "wrong-algorithm":
				method = jwt.SigningMethodHS512
			case "no-expiry":
				delete(claims, "exp")
			case "fractional-version":
				claims["ver"] = float64(s.tokenVersion) + 0.5
			}
			token, err := jwt.NewWithClaims(method, claims).SignedString(s.secret)
			if err != nil {
				t.Fatal(err)
			}
			if got := s.verifyToken(token); got != (kind == "valid") {
				t.Fatalf("verifyToken = %v", got)
			}
		})
	}
}
