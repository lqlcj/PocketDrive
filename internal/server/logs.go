package server

// 错误日志相关的 HTTP 层:恢复 panic、把 5xx 记下来、以及网页上查看/
// 清空/下载今天的日志。

import (
	"net/http"
	"runtime/debug"

	"pocketdrive/internal/httpx"
	"pocketdrive/internal/logs"
)

// statusRecorder 记住实际写出的状态码,用来判断要不要记日志。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Flush / Hijack 在这里不实现:本项目没有 SSE 和 WebSocket,文件下载走
// 的是 io.Copy,包一层不影响。真加了这类接口要记得在这补上透传。

// observe 是最外层的中间件:
//   - panic 不再只打到 stderr(net/http 自己会 recover 但不落我们的日志),
//     而是记一条带堆栈的错误,并给前端一个正常的 JSON 500;
//   - 任何 5xx 都记一行,这类问题用户描述不清、事后也查不到。
func observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if v := recover(); v != nil {
				logs.Errorf("panic", "%s %s: %v\n%s",
					r.Method, r.URL.Path, v, debug.Stack())
				if rec.status == http.StatusOK {
					httpx.Err(rec, http.StatusInternalServerError, "服务器内部错误")
				}
				return
			}
			if rec.status >= 500 {
				logs.Errorf("http", "%s %s -> %d", r.Method, r.URL.Path, rec.status)
			}
		}()
		next.ServeHTTP(rec, r)
	})
}

func handleLogs(w http.ResponseWriter, r *http.Request) {
	text, size := logs.Tail(0)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"enabled": logs.Enabled(),
		"text":    text,
		"size":    size,
	})
}

func handleLogsClear(w http.ResponseWriter, r *http.Request) {
	if err := logs.Clear(); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "清空失败: "+err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func handleLogsDownload(w http.ResponseWriter, r *http.Request) {
	p := logs.Path()
	if p == "" {
		httpx.Err(w, http.StatusNotFound, "当前部署没有日志文件")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="pocketdrive-error.log"`)
	http.ServeFile(w, r, p)
}
