package index

import (
	"testing"
	"testing/fstest"
)

func TestMediaCategoriesMatchPreview(t *testing.T) {
	svc := New(fstest.MapFS{
		"camera.MTS":     &fstest.MapFile{},
		"camera.m2ts":    &fstest.MapFile{},
		"source.ts":      &fstest.MapFile{},
		"icon.ico":       &fstest.MapFile{},
		"notes.markdown": &fstest.MapFile{},
	})
	for kind, want := range map[string]int{"video": 2, "image": 1, "markdown": 1} {
		if got := len(svc.Category(kind, 100)); got != want {
			t.Errorf("Category(%q) count = %d, want %d", kind, got, want)
		}
	}
	items := svc.Search("camera", 100)
	if len(items) != 2 {
		t.Fatalf("camera search count = %d, want 2", len(items))
	}
	for _, item := range items {
		if item.Kind != "video" {
			t.Errorf("%s classified as %s", item.Name, item.Kind)
		}
	}
}
