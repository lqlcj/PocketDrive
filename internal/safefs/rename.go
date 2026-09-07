// Package safefs coordinates file publication and non-overwriting moves.
package safefs

import (
	"errors"
	"os"
	"sync"
)

// Mutation serializes namespace changes made by the application. Linux also
// enforces no-replace in the kernel against changes from other processes.
var Mutation sync.Mutex

func Rename(root *os.Root, src, dst string) error {
	Mutation.Lock()
	defer Mutation.Unlock()
	return RenameLocked(root, src, dst)
}

func RenameLocked(root *os.Root, src, dst string) error {
	if src == dst {
		return nil
	}
	if _, err := root.Lstat(dst); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return renameNoReplace(root, src, dst)
}

func Replace(root *os.Root, src, dst string) error {
	Mutation.Lock()
	defer Mutation.Unlock()
	return root.Rename(src, dst)
}
