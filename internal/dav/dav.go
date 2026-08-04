// Package dav mounts the whole drive over WebDAV so phone clients
// (e.g. music players) can browse any subfolder directly. The
// filesystem combines the local data dir with all cloud storage
// mounts (@Name folders), matching what the web UI shows.
package dav

import (
	"net/http"

	"golang.org/x/net/webdav"

	"pocketdrive/internal/cloud"
)

func Handler(dataDir string, cloudSvc *cloud.Service) http.Handler {
	return &webdav.Handler{
		Prefix:     "/dav",
		FileSystem: cloud.NewDavFS(cloudSvc, dataDir),
		LockSystem: webdav.NewMemLS(),
	}
}
