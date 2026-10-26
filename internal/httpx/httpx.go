package httpx

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
)

type secureCookiesKey struct{}

// WithSecureCookies supports HTTPS termination at a reverse proxy without
// trusting client-controlled Forwarded or X-Forwarded-Proto headers.
func WithSecureCookies(force bool, next http.Handler) http.Handler {
	if !force {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), secureCookiesKey{}, true)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func CookieSecure(r *http.Request) bool {
	forced, _ := r.Context().Value(secureCookiesKey{}).(bool)
	return r.TLS != nil || forced
}

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func Err(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]string{"error": msg})
}

// Decode reads a JSON body capped at 1 MiB.
func Decode(r *http.Request, v any) error {
	return DecodeN(r, v, 1<<20)
}

// DecodeN reads a JSON body capped at n bytes (for bodies that carry
// small files, e.g. base64 .torrent uploads).
func DecodeN(r *http.Request, v any, n int64) error {
	return json.NewDecoder(io.LimitReader(r.Body, n)).Decode(v)
}

func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
