package aria2

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pocketdrive/internal/db"
)

func TestParseImportedTrackers(t *testing.T) {
	got, err := parseTrackers([]byte("\uFEFF# list\r\n\r\n udp://tracker.example:80/announce \r\nhttps://tracker.example/announce?key=abc\n; comment\nudp://tracker.example:80/announce\n"))
	if err != nil || got != "udp://tracker.example:80/announce,https://tracker.example/announce?key=abc" {
		t.Fatalf("list = %q, err = %v", got, err)
	}
	for _, raw := range []string{"", "# comment", "<html>error</html>", "file:///tmp/test", "udp://tracker.example/announce", "udp://tracker.example:99999/announce", "https://user:pass@tracker.example/announce", "https://tracker.example/a,b", "https://tracker.example/a b", "https://tracker.example/a\x00", "\xff\xfe", "https://[bad"} {
		if _, err := parseTrackers([]byte(raw)); err == nil {
			t.Errorf("accepted invalid list %q", raw)
		}
	}
}

func TestCustomTrackersImportPersistenceAndReset(t *testing.T) {
	m, _ := newTestManager(t)
	const fallback = "https://default.example/announce"
	if err := m.db.Save(&db.Setting{Key: trackersKey, Value: fallback}).Error; err != nil {
		t.Fatal(err)
	}
	s := defaultSettings()
	s.TrackerAuto = false
	if err := m.saveSettings(s); err != nil {
		t.Fatal(err)
	}
	const custom = "udp://custom.example:80/announce"
	w := httptest.NewRecorder()
	m.HandleImportTrackers(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(custom)))
	if w.Code != http.StatusOK {
		t.Fatalf("import: %d %s", w.Code, w.Body.String())
	}
	var response struct {
		Source string `json:"trackerSource"`
		Count  int    `json:"trackerCount"`
		At     string `json:"trackerUpdatedAt"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Source != "custom" || response.Count != 1 || response.At == "" {
		t.Fatalf("unexpected import response: %+v", response)
	}
	// A new manager must use the persisted import even with daily updates off.
	reloaded := NewManager(m.db, m.c, m.dataRoot, m.localDir)
	if reloaded.trackers() != custom || reloaded.taskOpts("", true)["bt-tracker"] != custom {
		t.Fatal("import not applied to metadata resolution and BT task options")
	}
	if _, ok := reloaded.taskOpts("", false)["bt-tracker"]; ok {
		t.Fatal("trackers applied to non-BT task")
	}
	// Updating the default cache must not replace the selected custom list.
	const updated = "https://new-default.example/announce"
	if err := m.db.Save(&db.Setting{Key: trackersKey, Value: updated}).Error; err != nil {
		t.Fatal(err)
	}
	if m.trackers() != custom {
		t.Fatal("default cache replaced custom trackers")
	}
	for _, raw := range []string{"", "invalid", strings.Repeat("x", maxTrackerBytes+1)} {
		w = httptest.NewRecorder()
		m.HandleImportTrackers(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(raw)))
		want := http.StatusBadRequest
		if len(raw) > maxTrackerBytes {
			want = http.StatusRequestEntityTooLarge
		}
		if w.Code != want || m.trackers() != custom {
			t.Fatalf("bad import changed list or returned wrong status: %d", w.Code)
		}
	}
	w = httptest.NewRecorder()
	m.HandleResetTrackers(w, httptest.NewRequest(http.MethodDelete, "/", nil))
	if w.Code != http.StatusOK || m.trackers() != updated {
		t.Fatal("reset did not restore latest default cache")
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Source != "default" {
		t.Fatalf("reset response: %+v, %v", response, err)
	}
}
