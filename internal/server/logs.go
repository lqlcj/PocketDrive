package server

import (
	"net/http"

	"pocketdrive/internal/httpx"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if v := recover(); v != nil {
				if rec.status == http.StatusOK {
					httpx.Err(rec, http.StatusInternalServerError, "服务器内部错误")
				}
				return
			}
		}()
		next.ServeHTTP(rec, r)
	})
}