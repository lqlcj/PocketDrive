package httpx

import (
	"encoding/json"
	"io"
	"net"
	"net/http"

	"pocketdrive/internal/logs"
)

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func Err(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]string{"error": msg})
}

// ErrLog 和 Err 一样返回错误,同时记一条错误日志。
// 用在「用户看到的提示不足以排查」的地方:请求带上下文(哪个路径、哪个
// 存储),而返回给前端的 msg 往往被刻意简化过。
func ErrLog(w http.ResponseWriter, r *http.Request, status int, component, msg string, cause error) {
	detail := msg
	if cause != nil {
		detail += ": " + cause.Error()
	}
	logs.Errorf(component, "%s %s -> %d %s", r.Method, r.URL.Path, status, detail)
	Err(w, status, msg)
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
