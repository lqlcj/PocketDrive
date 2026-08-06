package httpx

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
)

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
