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
	"os"
	"path"
	"strings"

	"golang.org/x/net/webdav"

	"pocketdrive/internal/cloud"
	"pocketdrive/internal/files"
)

const prefix = "/dav"

func Handler(root *os.Root, cloudSvc *cloud.Service, trash ...func(string) error) http.Handler {
	return &handler{
		dav: &webdav.Handler{
			Prefix:     prefix,
			FileSystem: cloud.NewDavFSRoot(cloudSvc, root, trash...),
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
	if r.Method == http.MethodPut {
		r = cloud.TrackDAVUpload(r)
	}
	if r.Method == "MOVE" || r.Method == "COPY" {
		r = r.Clone(r.Context())
		r.Header.Set("Overwrite", "F")
	}
	// WebDAV shares the application origin. Never let an uploaded HTML/SVG/etc.
	// execute in that origin when fetched with an ordinary browser GET.
	if r.Method == http.MethodGet {
		files.SetDownloadHeaders(w, path.Base(r.URL.Path), false, false)
	}
	if r.Method == methodPropfind {
		r = preparePropfind(r)
	}
	// 只重定向 GET。HEAD 照旧本地应答:它不传 body,中转不花流量,而
	// 客户端常拿 HEAD 探大小/类型——留在本地对不跟随重定向的客户端更友好。
	if r.Method == http.MethodGet && h.redirectToBucket(w, r) {
		return
	}
	h.dav.ServeHTTP(w, r)
}

const methodPropfind = "PROPFIND"

// preparePropfind 处理列目录请求的两件事,都是为了让上千个文件的文件夹
// 在手机上还能打开。
//
//  1. 不带 Depth 的 PROPFIND,RFC 4918 说按 infinity 处理,x/net/webdav
//     照做——对着挂载点就是把整个桶递归列一遍。真按这个来,稍大的存储
//     都会让客户端等到超时。RFC 4918 §9.1 本来就允许服务端拒绝
//     infinity;比起回 403,直接按文件浏览器真正想要的 Depth: 1 应答,
//     对客户端更友好。显式写了 Depth 的请求不动。
//  2. 挂上目录列表缓存,把遍历期间成百上千次回源 Stat 压成零次
//     (internal/cloud/davcache.go)。PROPFIND 是只读的,请求内缓存不会
//     让客户端读到自己刚写的旧值。
func preparePropfind(r *http.Request) *http.Request {
	if r.Header.Get("Depth") == "" {
		r.Header.Set("Depth", "1")
	}
	return r.WithContext(cloud.WithListCache(r.Context()))
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
	u, err := m.PresignGet(r.Context(), rel, path.Base(rel), files.NeedsAttachment(rel))
	if err != nil || u == "" {
		return false // 签名失败或挂载不支持直连时退回中转
	}
	http.Redirect(w, r, u, http.StatusFound)
	return true
}
