package server

import (
	"log"
	"net/http"
	"runtime/debug"

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
				// 这里已经 recover 了,net/http 不会再替我们打印 panic。
				// 即使不落盘,也必须把堆栈写到容器 stdout/stderr 供排障。
				log.Printf("panic: %s %s: %v\n%s", r.Method, r.URL.Path, v, debug.Stack())
				if rec.status == http.StatusOK {
					httpx.Err(rec, http.StatusInternalServerError, "服务器内部错误")
				}
				return
			}
		}()
		next.ServeHTTP(rec, r)
	})
}
