package cloud

import (
	"context"
	"os"
	"path"
	"strings"

	"golang.org/x/net/webdav"
	"pocketdrive/internal/safefs"
)

// rootFS adapts os.Root to webdav.FileSystem. Unlike webdav.Dir, os.Root
// rejects symbolic links that resolve outside the data tree.
type rootFS struct {
	root  *os.Root
	trash func(string) error
}

func rootName(name string) (string, error) {
	if strings.ContainsRune(name, 0) || strings.Contains(name, `\`) {
		return "", os.ErrNotExist
	}
	clean := strings.TrimPrefix(path.Clean("/"+name), "/")
	for _, part := range strings.Split(clean, "/") {
		if part == ".trash" || part == ".pocketdrive" || strings.HasPrefix(part, ".pd-write-") {
			return "", os.ErrPermission
		}
	}
	if clean == "" {
		return ".", nil
	}
	return clean, nil
}

func (f rootFS) Mkdir(_ context.Context, name string, perm os.FileMode) error {
	n, err := rootName(name)
	if err != nil || n == "." {
		return os.ErrInvalid
	}
	return f.root.Mkdir(n, perm)
}

func (f rootFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	n, err := rootName(name)
	if err != nil {
		return nil, err
	}
	if isWriteOpen(flag) {
		return newAtomicDAVFile(ctx, f.root, n, perm)
	}
	file, err := f.root.OpenFile(n, flag, perm)
	if err != nil {
		return nil, err
	}
	return &visibleDAVFile{File: file}, nil
}

func (f rootFS) RemoveAll(_ context.Context, name string) error {
	n, err := rootName(name)
	if err != nil {
		return err
	}
	if n == "." {
		return os.ErrInvalid
	}
	if f.trash == nil {
		return os.ErrPermission
	}
	return f.trash(n)
}

func (f rootFS) Rename(_ context.Context, oldName, newName string) error {
	old, err := rootName(oldName)
	if err != nil {
		return err
	}
	newPath, err := rootName(newName)
	if err != nil {
		return err
	}
	if old == "." || newPath == "." {
		return os.ErrInvalid
	}
	return safefs.Rename(f.root, old, newPath)
}

func (f rootFS) Stat(_ context.Context, name string) (os.FileInfo, error) {
	n, err := rootName(name)
	if err != nil {
		return nil, err
	}
	return f.root.Stat(n)
}
