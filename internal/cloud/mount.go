package cloud

import (
	"context"
	"fmt"
	"io"
	"time"

	"pocketdrive/internal/db"
)

// Mount is the file interface shared by external storage backends.
type Mount interface {
	MountName() string
	Ping(context.Context) error
	List(context.Context, string) ([]Entry, error)
	Stat(context.Context, string) (Entry, error)
	Open(context.Context, string) (io.ReadSeekCloser, Entry, error)
	Put(context.Context, string, io.Reader, int64) error
	Mkdir(context.Context, string) error
	Delete(context.Context, string) error
	Rename(context.Context, string, string) error
	WalkFiles(context.Context, string, func(string, int64, time.Time) error) error
	// An empty URL means the caller must proxy the file through Open.
	PresignGet(context.Context, string, string, bool) (string, error)
}

func newMount(p *db.StoragePolicy) (Mount, error) {
	switch p.Type {
	case "", "s3":
		return newS3Mount(p)
	case "webdav":
		return newWebDAVMount(p)
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", p.Type)
	}
}

func (m *S3Mount) MountName() string { return m.Name }
