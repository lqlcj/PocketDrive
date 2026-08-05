// Package dav mounts the whole drive over WebDAV so phone clients
// (e.g. music players) can browse any subfolder directly. The
// filesystem combines the local data dir with all cloud storage
// mounts (@Name folders), matching what the web UI shows.
//
// 读 @挂载 里的文件默认不经 VPS 中转:GET 直接 302 到预签名 URL,
// 播放器自己连存储桶(见 redirectToBucket)。
package dav

import (
	"net/http"
	"path"
	"strings"

	"golang.org/x/net/webdav"

	"pocketdrive/internal/cloud"
)

const prefix = "/dav"

func Handler(dataDir string, cloudSvc *cloud.Service) http.Handler {
	return &handler{
		dav: &webdav.Handler{
			Prefix:     prefix,
			FileSystem: cloud.NewDavFS(cloudSvc, dataDir),
			LockSystem: webdav.NewMemLS(),
		},
		cloud: cloudSvc,
	}
}

type handler struct {
	dav   *webdav.Handler
	cloud *cloud.Service
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 只重定向 GET。HEAD 照旧本地应答:它不传 body,中转不花流量,而
	// 客户端常拿 HEAD 探大小/类型——留在本地对不跟随重定向的客户端更友好。
	if r.Method == http.MethodGet && h.redirectToBucket(w, r) {
		return
	}
	h.dav.ServeHTTP(w, r)
}

// redirectToBucket 把外部存储里的文件读取 302 到预签名 URL,字节不过
// VPS。返回 false 表示这个请求仍旧交给 webdav.Handler 中转。
//
// 任何拿不准的情况都返回 false:宁可多走一趟中转,也不要把客户端甩到
// 桶上收一个 XML 报错。
func (h *handler) redirectToBucket(w http.ResponseWriter, r *http.Request) bool {
	if !h.cloud.DavDirect() {
		return false
	}
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	if len(rest) == len(r.URL.Path) {
		return false // 没有 /dav 前缀,不该走到这
	}
	if strings.HasSuffix(rest, "/") {
		return false // 集合(目录)没有预签名一说
	}
	p := strings.Trim(path.Clean("/"+rest), "/")
	if !cloud.IsMountPath(p) {
		return false // 本机文件
	}
	m, rel, ok := h.cloud.Resolve(p)
	if !ok || rel == "" {
		return false // 挂载不存在,或指向挂载点本身(是个目录)
	}
	// 目录和不存在的对象交回 webdav,让它给出正常的 405 / 404
	if e, err := m.Stat(r.Context(), rel); err != nil || e.Dir {
		return false
	}
	u, err := m.PresignGet(r.Context(), rel, path.Base(rel), false)
	if err != nil {
		return false // 签名失败就退回中转,别让播放直接断掉
	}
	http.Redirect(w, r, u, http.StatusFound)
	return true
}
