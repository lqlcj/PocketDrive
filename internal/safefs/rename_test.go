package safefs

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentRenamePreservesBothSources(t *testing.T) {
	for _, directory := range []bool{false, true} {
		dir := t.TempDir()
		root, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"a", "b"} {
			if directory {
				err = root.Mkdir(name, 0700)
			} else {
				err = os.WriteFile(filepath.Join(dir, name), []byte(name), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for _, name := range []string{"a", "b"} {
			wg.Go(func() { results <- Rename(root, name, "target") })
		}
		wg.Wait()
		close(results)
		success, conflicts := 0, 0
		for err := range results {
			if err == nil {
				success++
			} else if errors.Is(err, os.ErrExist) {
				conflicts++
			} else {
				t.Fatal(err)
			}
		}
		if success != 1 || conflicts != 1 {
			t.Fatalf("success=%d conflicts=%d", success, conflicts)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 2 {
			t.Fatalf("entries=%v err=%v", entries, err)
		}
		root.Close()
	}
}

func TestRenameRejectsDanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.WriteFile(filepath.Join(dir, "source"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing", filepath.Join(dir, "target")); err != nil {
		t.Skip(err)
	}
	if err := Rename(root, "source", "target"); !errors.Is(err, os.ErrExist) {
		t.Fatalf("error=%v", err)
	}
}
