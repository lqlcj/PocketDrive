package updater

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDisabled(t *testing.T) {
	if New("", "").Enabled() {
		t.Fatal("empty updater config must be disabled")
	}
	if err := New("", "").Trigger(context.Background()); err == nil {
		t.Fatal("disabled updater must reject triggers")
	}
}

func TestTriggerUsesFixedAuthenticatedEndpoint(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodPost || r.URL.Path != "/v1/update" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Fatalf("unexpected authorization: %q", got)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	s := New(srv.URL+"/", "secret-token")
	if err := s.Trigger(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("updater was not called")
	}
	if err := s.Trigger(context.Background()); err == nil {
		t.Fatal("successful triggers must be rate limited")
	}
}

func TestFailedTriggerCanBeRetried(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	s := New(srv.URL, "secret-token")
	if err := s.Trigger(context.Background()); err == nil {
		t.Fatal("first trigger should fail")
	}
	if err := s.Trigger(context.Background()); err != nil {
		t.Fatalf("retry after failure should succeed: %v", err)
	}
}
