package trash

import (
	"os"
	"path/filepath"
	"testing"

	"pocketdrive/internal/db"
	"pocketdrive/internal/files"
)

type usageTracker struct {
	delta int64
}

func (*usageTracker) CheckLocal(int64) error { return nil }
func (u *usageTracker) AddUsage(delta int64) { u.delta += delta }
func (*usageTracker) UploadLimit() int64     { return 0 }

func TestPermDeleteDirectoryAdjustsActualUsage(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	if err := os.MkdirAll(filepath.Join(data, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "folder", "a.bin"), []byte("123"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "folder", "b.bin"), []byte("12345"), 0o644); err != nil {
		t.Fatal(err)
	}

	gdb, err := db.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	fs, err := files.New(data, filepath.Join(root, "uploads"), nil, gdb)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Root().Close() })
	usage := &usageTracker{}
	fs.SetLocalSpace(usage)
	s := New(gdb, fs, nil)

	if err := s.Trash("folder"); err != nil {
		t.Fatal(err)
	}
	var item db.TrashItem
	if err := gdb.First(&item).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.permDelete(&item); err != nil {
		t.Fatal(err)
	}
	if usage.delta != -8 {
		t.Fatalf("usage delta = %d, want -8", usage.delta)
	}
	var count int64
	if err := gdb.Model(&db.TrashItem{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("trash rows = %d, want 0", count)
	}
}
