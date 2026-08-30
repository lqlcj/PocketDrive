package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Handler serves the embedded SPA build. Unknown paths (client-side
// routes) fall back to index.html; real assets are served as-is.
//
// 缓存:embed.FS 里的文件 modtime 是零值,http.FileServer 因此既不发
// Last-Modified 也不发 ETag——不显式给 Cache-Control 的话,浏览器每次
// 刷新都要把所有 JS/CSS/字体重下一遍。Vite 产物在 assets/ 下且文件名
// 带内容 hash,内容变了路径就变,可以放心长期缓存;其余(index.html、
// 图标)必须每次回来问一次,不然发版后用户会一直卡在旧页面上。
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if f, err := sub.Open(p); err == nil {
				f.Close()
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else {
					w.Header().Set("Cache-Control", "no-cache")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
