package share

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pocketdrive/internal/httpx"
)

func TestShareCookieRespectsHTTPSPolicy(t *testing.T) {
	s := newLocalTestService(t)
	sh, err := s.CreateText("private text", "share-password", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, scheme  string
		force, secure bool
	}{
		{"http", "http", false, false},
		{"tls", "https", false, true},
		{"https-proxy", "http", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body bytes.Buffer
			_ = json.NewEncoder(&body).Encode(map[string]string{"password": "share-password"})
			r := httptest.NewRequest(http.MethodPost, tc.scheme+"://drive.example/unlock", &body)
			r.SetPathValue("token", sh.Token)
			w := httptest.NewRecorder()
			httpx.WithSecureCookies(tc.force, http.HandlerFunc(s.HandleUnlock)).ServeHTTP(w, r)
			cookies := w.Result().Cookies()
			if w.Code != http.StatusOK || len(cookies) != 1 {
				t.Fatalf("status=%d, cookies=%d", w.Code, len(cookies))
			}
			c := cookies[0]
			if !c.HttpOnly || c.Secure != tc.secure || c.SameSite != http.SameSiteLaxMode ||
				c.Path != "/api/v1/public/share/"+sh.Token || c.MaxAge != 15*60 {
				t.Fatalf("incorrect share cookie flags: %+v", c)
			}
			info := httptest.NewRequest(http.MethodGet, "/info", nil)
			info.AddCookie(c)
			if err := s.authorized(info, sh); err != nil {
				t.Fatalf("issued cookie does not unlock share: %v", err)
			}
		})
	}
}
