package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pocketdrive/internal/config"
	"pocketdrive/internal/httpx"
)

// This is the same public liveness route used by deployments and reverse
// proxies. Keep a real HTTP smoke test in-tree so release verification does not
// depend on Docker or a background process being available on the workstation.
func TestPingSmoke(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/ping", func(w http.ResponseWriter, _ *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]string{
			"status": "ok", "version": config.Version,
		})
	})
	srv := httptest.NewServer(securityHeaders(mux))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/api/v1/ping")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" || body["version"] != config.Version {
		t.Fatalf("ping = %#v", body)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", got)
	}
}
