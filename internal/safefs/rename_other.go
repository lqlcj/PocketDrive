//go:build !linux

package safefs

import "os"

func renameNoReplace(root *os.Root, src, dst string) error {
	info, err := root.Lstat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		// Linking fails atomically if dst exists. If unlinking fails, leave both
		// names intact rather than risking loss of either file.
		if err := root.Link(src, dst); err != nil {
			return err
		}
		return root.Remove(src)
	}
	return root.Rename(src, dst)
}
