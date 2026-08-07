package server

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
