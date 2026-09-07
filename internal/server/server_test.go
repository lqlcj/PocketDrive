package server

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pocketdrive/internal/files"
)

func TestPDFPreviewAllowsOnlySameOriginFrames(t *testing.T) {
	for _, tc := range []struct {
		name     string
		download bool
		frame    string
	}{
		{"document.pdf", false, "SAMEORIGIN"},
		{"document.PDF", false, "SAMEORIGIN"},
		{"document.pdf", true, "DENY"},
		{"document.html", false, "DENY"},
		{"document.svg", false, "DENY"},
	} {
		t.Run(tc.name+tc.frame, func(t *testing.T) {
			h := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				files.SetDownloadHeaders(w, tc.name, tc.download, false)
				w.WriteHeader(http.StatusOK)
			}))
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/download", nil))
			if got := w.Header().Get("X-Frame-Options"); got != tc.frame {
				t.Fatalf("X-Frame-Options = %q, want %q", got, tc.frame)
			}
			if tc.frame == "SAMEORIGIN" {
				if got := w.Header().Get("Content-Security-Policy"); got != "frame-ancestors 'self'" {
					t.Errorf("CSP = %q", got)
				}
				if got := w.Header().Get("Content-Type"); got != "application/pdf" {
					t.Errorf("Content-Type = %q", got)
				}
			}
		})
	}
}

func TestDownloadListRouteRegistered(t *testing.T) {
	mux := http.NewServeMux()
	registerDownloadRoutes(mux, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/downloads", nil)
	_, pattern := mux.Handler(req)
	if pattern != "GET /api/v1/downloads" {
		t.Fatalf("GET download list route pattern = %q", pattern)
	}
}

func TestObserveLogsRecoveredPanic(t *testing.T) {
	var logs bytes.Buffer
	oldWriter := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(oldWriter) })

	h := observe(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/explode", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if !strings.Contains(logs.String(), "boom") || !strings.Contains(logs.String(), "/explode") {
		t.Fatalf("panic was not logged with context: %q", logs.String())
	}
}
