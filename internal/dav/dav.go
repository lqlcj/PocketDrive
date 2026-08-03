// Package dav mounts the whole data directory over WebDAV so phone
// clients (e.g. music players) can browse any subfolder directly.
package dav

import (
	"net/http"

	"golang.org/x/net/webdav"
)

func Handler(dataDir string) http.Handler {
	return &webdav.Handler{
		Prefix:     "/dav",
		FileSystem: webdav.Dir(dataDir),
		LockSystem: webdav.NewMemLS(),
	}
}
