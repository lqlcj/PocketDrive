package files

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenameAndMoveRejectExistingDestination(t *testing.T) {
	for _, move := range []bool{false, true} {
		s, data, _ := newFileTestService(t)
		if err := os.Mkdir(filepath.Join(data, "dest"), 0755); err != nil {
			t.Fatal(err)
		}
		for name, content := range map[string]string{"source.txt": "source", "dest/source.txt": "destination", "target.txt": "destination"} {
			if err := os.WriteFile(filepath.Join(data, name), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
		}
		w := httptest.NewRecorder()
		dst := "target.txt"
		if move {
			dst = "dest/source.txt"
			s.HandleMove(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"path":"source.txt","dest":"dest"}`)))
		} else {
			s.HandleRename(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"path":"source.txt","newName":"target.txt"}`)))
		}
		if w.Code != http.StatusConflict {
			t.Fatalf("move=%v: status=%d: %s", move, w.Code, w.Body.String())
		}
		for name, expected := range map[string]string{"source.txt": "source", dst: "destination"} {
			got, err := os.ReadFile(filepath.Join(data, name))
			if err != nil || string(got) != expected {
				t.Fatalf("%s = %q, %v", name, got, err)
			}
		}
	}
}
