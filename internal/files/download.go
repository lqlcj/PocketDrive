package files

import (
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// activeWebExtensions are formats a browser can interpret as active web
// content. Serving one inline from the application origin would let an
// uploaded file run with the same origin as PocketDrive.
var activeWebExtensions = map[string]struct{}{
	".css": {}, ".htm": {}, ".html": {}, ".mht": {}, ".mhtml": {},
	".shtm": {}, ".shtml": {}, ".svg": {}, ".svgz": {}, ".swf": {},
	".xht": {}, ".xhtml": {}, ".xml": {}, ".xsl": {}, ".xslt": {},
	".js": {}, ".jsx": {}, ".mjs": {}, ".cjs": {}, ".ts": {}, ".tsx": {},
	".wasm": {},
}

// NeedsAttachment reports whether name must never be rendered inline by a
// browser. It is exported so public shares and S3 redirects use the same rule.
func NeedsAttachment(name string) bool {
	_, ok := activeWebExtensions[strings.ToLower(path.Ext(name))]
	return ok
}

// SetDownloadHeaders applies the common response policy for drive downloads
// and public shares. Unknown extensions are typed as opaque bytes so
// ServeContent cannot sniff an extensionless HTML document into active content.
func SetDownloadHeaders(w http.ResponseWriter, name string, download, inline bool) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	ext := strings.ToLower(path.Ext(name))
	active := NeedsAttachment(name)
	if active {
		download = true
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Security-Policy",
			"sandbox; default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		w.Header().Set("Cache-Control", "no-store")
	} else if ext == "" || mime.TypeByExtension(ext) == "" {
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	disposition := ""
	if download {
		disposition = "attachment"
	} else if inline {
		disposition = "inline"
	}
	if disposition != "" {
		w.Header().Set("Content-Disposition",
			disposition+"; filename*=UTF-8''"+url.PathEscape(name))
	}
}
