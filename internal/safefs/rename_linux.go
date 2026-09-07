package safefs

import (
	"os"
	"path"

	"golang.org/x/sys/unix"
)

func renameNoReplace(root *os.Root, src, dst string) error {
	from, err := root.Open(path.Dir(src))
	if err != nil {
		return err
	}
	defer from.Close()
	to, err := root.Open(path.Dir(dst))
	if err != nil {
		return err
	}
	defer to.Close()
	return unix.Renameat2(int(from.Fd()), path.Base(src), int(to.Fd()), path.Base(dst), unix.RENAME_NOREPLACE)
}
