package cloud

import (
	"context"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDAVWriteFailureDoesNotReplaceOriginal(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	r := TrackDAVUpload(httptest.NewRequest("PUT", "/file", strings.NewReader("new")))
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		t.Fatal(err)
	}
	f, err := newAtomicDAVFile(r.Context(), root, "file", 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.f.Close()
	if _, err := f.Write([]byte("new")); err == nil {
		t.Fatal("expected disk write failure")
	}
	if err := f.Close(); err == nil {
		t.Fatal("failed write was committed")
	}
	got, err := os.ReadFile(filepath.Join(dir, "file"))
	if err != nil || string(got) != "original" {
		t.Fatalf("original damaged: %q %v", got, err)
	}
}

func TestDAVCopyDoesNotReplaceConcurrentDestination(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	f, err := newAtomicDAVFile(context.Background(), root, "file", 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("copy")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("concurrent"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); !os.IsExist(err) {
		t.Fatalf("want conflict, got %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "file"))
	if err != nil || string(got) != "concurrent" {
		t.Fatalf("destination damaged: %q %v", got, err)
	}
}
