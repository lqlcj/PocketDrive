package cloud

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path"

	"golang.org/x/net/webdav"
	"pocketdrive/internal/safefs"
)

type uploadKey struct{}

type uploadBody struct {
	io.ReadCloser
	expected int64
	read     int64
	eof      bool
	err      error
}

func (b *uploadBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.read += int64(n)
	if err == io.EOF {
		b.eof = true
	} else if err != nil {
		b.err = err
	}
	return n, err
}

// TrackDAVUpload lets Close observe input errors: x/net/webdav closes the
// destination even when io.Copy failed while reading the request body.
func TrackDAVUpload(r *http.Request) *http.Request {
	b := &uploadBody{ReadCloser: r.Body, expected: r.ContentLength}
	r = r.WithContext(context.WithValue(r.Context(), uploadKey{}, b))
	r.Body = b
	return r
}

func uploadError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b, ok := ctx.Value(uploadKey{}).(*uploadBody); ok {
		if b.err != nil {
			return b.err
		}
		if !b.eof || (b.expected >= 0 && b.read != b.expected) {
			return io.ErrUnexpectedEOF
		}
	}
	return nil
}

type atomicDAVFile struct {
	f         *os.File
	root      *os.Root
	ctx       context.Context
	tmp, name string
	err       error
	closed    bool
}

func newAtomicDAVFile(ctx context.Context, root *os.Root, name string, perm os.FileMode) (*atomicDAVFile, error) {
	if fi, err := root.Stat(name); err == nil {
		if !fi.Mode().IsRegular() {
			return nil, os.ErrPermission
		}
		perm = fi.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	tmp := path.Join(path.Dir(name), ".pd-write-"+hex.EncodeToString(token[:])+".tmp")
	f, err := root.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return nil, err
	}
	return &atomicDAVFile{f: f, root: root, ctx: ctx, tmp: tmp, name: name}, nil
}

func (f *atomicDAVFile) Write(p []byte) (int, error) {
	n, err := f.f.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		f.err = err
	}
	return n, err
}

func (f *atomicDAVFile) Read(p []byte) (int, error)              { return f.f.Read(p) }
func (f *atomicDAVFile) Seek(n int64, whence int) (int64, error) { return f.f.Seek(n, whence) }
func (f *atomicDAVFile) Readdir(n int) ([]os.FileInfo, error)    { return f.f.Readdir(n) }
func (f *atomicDAVFile) Stat() (os.FileInfo, error) {
	fi, err := f.f.Stat()
	if err != nil {
		f.err = err
		return nil, err
	}
	return davFileInfo{FileInfo: fi, name: path.Base(f.name)}, nil
}

type davFileInfo struct {
	os.FileInfo
	name string
}

func (f davFileInfo) Name() string { return f.name }

func (f *atomicDAVFile) Close() error {
	if f.closed {
		return f.err
	}
	f.closed = true
	defer f.root.Remove(f.tmp)
	if f.err == nil {
		f.err = uploadError(f.ctx)
	}
	if f.err == nil {
		f.err = f.f.Sync()
	}
	if err := f.f.Close(); f.err == nil {
		f.err = err
	}
	if f.err == nil {
		if _, replacingUpload := f.ctx.Value(uploadKey{}).(*uploadBody); replacingUpload {
			f.err = safefs.Replace(f.root, f.tmp, f.name)
		} else {
			f.err = safefs.Rename(f.root, f.tmp, f.name)
		}
	}
	return f.err
}

type visibleDAVFile struct{ webdav.File }

func (f *visibleDAVFile) Readdir(n int) ([]os.FileInfo, error) {
	var out []os.FileInfo
	for {
		count := n
		if n > 0 {
			count -= len(out)
		}
		entries, err := f.File.Readdir(count)
		for _, entry := range entries {
			if _, denied := rootName(entry.Name()); denied == nil {
				out = append(out, entry)
			}
		}
		if n <= 0 || len(out) == n || err != nil {
			return out, err
		}
	}
}
