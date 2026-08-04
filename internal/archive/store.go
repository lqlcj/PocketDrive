package archive

// 压缩/解压需要读写文件,而文件可能在本机磁盘,也可能在 @外部存储。
// 这里把两者收敛成同一套最小接口,上层的打包/解包逻辑就不必到处分叉。

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"

	"pocketdrive/internal/cloud"
	"pocketdrive/internal/files"
)

type entry struct {
	size  int64
	mtime time.Time
	dir   bool
}

// vfs 是压缩/解压看到的存储视图。路径一律是相对该存储根的斜杠路径。
type vfs interface {
	stat(ctx context.Context, p string) (entry, error)
	// walkFiles 深度遍历 p 下的所有文件(不含目录本身),rel 相对 p
	walkFiles(ctx context.Context, p string, fn func(rel string, e entry) error) error
	open(ctx context.Context, p string) (io.ReadCloser, error)
	// create 写入文件,父目录自动创建;size 未知传 -1
	create(ctx context.Context, p string, r io.Reader, size int64) error
	mkdirAll(ctx context.Context, p string) error
}

// resolve 按路径挑选存储:@开头走外部存储,否则本机。
// 返回的 rel 是相对该存储根的路径。
func (s *Service) resolve(p string) (vfs, string, error) {
	p = files.CleanPath(p)
	if cloud.IsMountPath(p) {
		m, rel, ok := s.cloud.Resolve(p)
		if !ok {
			return nil, "", errors.New("外部存储不存在或未挂载")
		}
		return mountFS{m}, rel, nil
	}
	return localFS{s.files.Root()}, p, nil
}

// sameStore 判断两个路径是否在同一个存储里(跨存储的压缩/解压需要
// 中转,代价与语义都不同,一律拒绝)。
func sameStore(a, b string) bool {
	ma, mb := cloud.IsMountPath(a), cloud.IsMountPath(b)
	if ma != mb {
		return false
	}
	if !ma {
		return true
	}
	nameOf := func(p string) string {
		n, _, _ := strings.Cut(strings.TrimPrefix(files.CleanPath(p), "@"), "/")
		return n
	}
	return nameOf(a) == nameOf(b)
}

// ---- 本机存储:全部经 os.Root,天然拒绝越出数据目录 ----

type localFS struct{ root *os.Root }

// fsName 把干净路径转成 os.Root/fs.FS 期望的形式(根是 ".")。
func fsName(p string) string {
	if p == "" {
		return "."
	}
	return p
}

func (l localFS) stat(_ context.Context, p string) (entry, error) {
	fi, err := l.root.Stat(fsName(p))
	if err != nil {
		return entry{}, err
	}
	return entry{size: fi.Size(), mtime: fi.ModTime(), dir: fi.IsDir()}, nil
}

func (l localFS) walkFiles(_ context.Context, p string, fn func(string, entry) error) error {
	base := fsName(p)
	return fs.WalkDir(l.root.FS(), base, func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// 符号链接不跟随:压缩包里放一份链接指向的内容会让体积暴涨,
		// 指向数据目录之外时还会把外部文件泄进包里
		if !d.Type().IsRegular() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(name, base), "/")
		if base == "." {
			rel = name
		}
		return fn(rel, entry{size: fi.Size(), mtime: fi.ModTime()})
	})
}

func (l localFS) open(_ context.Context, p string) (io.ReadCloser, error) {
	return l.root.Open(fsName(p))
}

func (l localFS) mkdirAll(_ context.Context, p string) error {
	if p == "" {
		return nil
	}
	return l.root.MkdirAll(p, 0o755)
}

func (l localFS) create(_ context.Context, p string, r io.Reader, _ int64) error {
	if dir := path.Dir(p); dir != "." && dir != "" {
		if err := l.root.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := l.root.Create(p)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		_ = l.root.Remove(p)
		return err
	}
	return f.Close()
}

// ---- 外部存储 ----

type mountFS struct{ m *cloud.S3Mount }

func (x mountFS) stat(ctx context.Context, p string) (entry, error) {
	e, err := x.m.Stat(ctx, p)
	if err != nil {
		return entry{}, err
	}
	return entry{size: e.Size, mtime: time.UnixMilli(e.Mtime), dir: e.Dir}, nil
}

func (x mountFS) walkFiles(ctx context.Context, p string, fn func(string, entry) error) error {
	// p 本身是文件时 WalkFiles 前缀匹配不到,单独处理
	if e, err := x.m.Stat(ctx, p); err == nil && !e.Dir {
		return fn(path.Base(p), entry{size: e.Size, mtime: time.UnixMilli(e.Mtime)})
	}
	return x.m.WalkFiles(ctx, p, func(rel string, size int64, mtime time.Time) error {
		return fn(rel, entry{size: size, mtime: mtime})
	})
}

func (x mountFS) open(ctx context.Context, p string) (io.ReadCloser, error) {
	obj, _, err := x.m.Open(ctx, p)
	if err != nil {
		return nil, err
	}
	return obj, nil
}

// mkdirAll 对 S3 是个空操作:对象 key 里带斜杠就足以表达层级,
// 只有需要空目录可见时才写目录标记。
func (x mountFS) mkdirAll(ctx context.Context, p string) error {
	if p == "" {
		return nil
	}
	return x.m.Mkdir(ctx, p)
}

func (x mountFS) create(ctx context.Context, p string, r io.Reader, size int64) error {
	return x.m.Put(ctx, p, r, size)
}
