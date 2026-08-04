package cloud

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"

	"golang.org/x/net/webdav"
)

// DavFS 把本机目录和全部外部存储挂载合成一个 WebDAV 文件系统:
// 根目录 = 本机文件 + @挂载名 虚拟文件夹,手机播放器里看到的结构和
// 网页端完全一致。
type DavFS struct {
	svc   *Service
	local webdav.FileSystem
}

func NewDavFS(svc *Service, dataDir string) *DavFS {
	return &DavFS{svc: svc, local: webdav.Dir(dataDir)}
}

var errPerm = errors.New("webdav: operation not supported on cloud storage")

// split 解析 webdav 路径("/@R2/a/b" 等):挂载命中返回 (mount, rel)。
func (d *DavFS) split(name string) (*S3Mount, string, bool) {
	p := strings.Trim(path.Clean("/"+name), "/")
	if !IsMountPath(p) {
		return nil, "", false
	}
	m, rel, ok := d.svc.Resolve(p)
	if !ok {
		return nil, "", false
	}
	return m, rel, true
}

// isMountName 报告路径是否形如 @xxx(即使挂载不存在也算,避免把
// @ 前缀路径误交给本机文件系统)。
func isMountName(name string) bool {
	return IsMountPath(strings.Trim(path.Clean("/"+name), "/"))
}

func (d *DavFS) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	if m, rel, ok := d.split(name); ok {
		if rel == "" {
			return os.ErrExist
		}
		return m.Mkdir(ctx, rel)
	}
	if isMountName(name) {
		return os.ErrNotExist
	}
	return d.local.Mkdir(ctx, name, perm)
}

func (d *DavFS) RemoveAll(ctx context.Context, name string) error {
	if m, rel, ok := d.split(name); ok {
		if rel == "" {
			return errPerm // 删挂载点 = 删策略,去网页设置里做
		}
		return m.Delete(ctx, rel)
	}
	if isMountName(name) {
		return os.ErrNotExist
	}
	return d.local.RemoveAll(ctx, name)
}

func (d *DavFS) Rename(ctx context.Context, oldName, newName string) error {
	mo, orel, okO := d.split(oldName)
	mn, nrel, okN := d.split(newName)
	if okO || okN {
		if !okO || !okN || mo != mn {
			return errPerm // 跨存储移动不支持
		}
		if orel == "" || nrel == "" {
			return errPerm
		}
		return mo.Rename(ctx, orel, nrel)
	}
	if isMountName(oldName) || isMountName(newName) {
		return os.ErrNotExist
	}
	return d.local.Rename(ctx, oldName, newName)
}

func (d *DavFS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	if m, rel, ok := d.split(name); ok {
		e, err := m.Stat(ctx, rel)
		if err != nil {
			return nil, os.ErrNotExist
		}
		return entryInfo{e}, nil
	}
	if isMountName(name) {
		return nil, os.ErrNotExist
	}
	return d.local.Stat(ctx, name)
}

func (d *DavFS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	if m, rel, ok := d.split(name); ok {
		// 写入(PUT)
		if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE) != 0 {
			if rel == "" {
				return nil, errPerm
			}
			return newS3WriteFile(ctx, m, rel), nil
		}
		// 目录?
		if e, err := m.Stat(ctx, rel); err == nil && e.Dir {
			return &s3DirFile{ctx: ctx, m: m, rel: rel, info: entryInfo{e}}, nil
		}
		// 文件读取
		obj, e, err := m.Open(ctx, rel)
		if err != nil {
			return nil, os.ErrNotExist
		}
		return &s3ReadFile{obj: obj, info: entryInfo{e}}, nil
	}
	if isMountName(name) {
		return nil, os.ErrNotExist
	}
	f, err := d.local.OpenFile(ctx, name, flag, perm)
	if err != nil {
		return nil, err
	}
	// 根目录:Readdir 结果追加挂载点虚拟文件夹
	if strings.Trim(path.Clean("/"+name), "/") == "" {
		return &rootFile{File: f, svc: d.svc}, nil
	}
	return f, nil
}

// ---- FileInfo ----

type entryInfo struct{ e Entry }

func (i entryInfo) Name() string { return i.e.Name }
func (i entryInfo) Size() int64  { return i.e.Size }
func (i entryInfo) Mode() os.FileMode {
	if i.e.Dir {
		return os.ModeDir | 0o755
	}
	return 0o644
}
func (i entryInfo) ModTime() time.Time {
	if i.e.Mtime == 0 {
		return time.Unix(0, 0)
	}
	return time.UnixMilli(i.e.Mtime)
}
func (i entryInfo) IsDir() bool      { return i.e.Dir }
func (i entryInfo) Sys() interface{} { return nil }

// ---- 根目录包装:追加挂载点 ----

type rootFile struct {
	webdav.File
	svc  *Service
	done bool
}

func (f *rootFile) Readdir(count int) ([]fs.FileInfo, error) {
	infos, err := f.File.Readdir(count)
	if count > 0 {
		// 分页读取的客户端极少;挂载点在首批之后一次性补上
		if err == io.EOF && !f.done {
			f.done = true
			return append(infos, f.mountInfos()...), nil
		}
		return infos, err
	}
	if err != nil {
		return infos, err
	}
	if !f.done {
		f.done = true
		infos = append(infos, f.mountInfos()...)
	}
	return infos, nil
}

func (f *rootFile) mountInfos() []fs.FileInfo {
	names := f.svc.Names()
	out := make([]fs.FileInfo, 0, len(names))
	for _, n := range names {
		out = append(out, entryInfo{Entry{Name: MountPrefix + n, Dir: true}})
	}
	return out
}

// ---- S3 目录 ----

type s3DirFile struct {
	ctx  context.Context
	m    *S3Mount
	rel  string
	info os.FileInfo
	read bool
}

func (f *s3DirFile) Close() error                 { return nil }
func (f *s3DirFile) Read([]byte) (int, error)     { return 0, errPerm }
func (f *s3DirFile) Write([]byte) (int, error)    { return 0, errPerm }
func (f *s3DirFile) Seek(int64, int) (int64, error) { return 0, errPerm }
func (f *s3DirFile) Stat() (os.FileInfo, error)   { return f.info, nil }

func (f *s3DirFile) Readdir(count int) ([]fs.FileInfo, error) {
	if f.read {
		return nil, io.EOF
	}
	f.read = true
	entries, err := f.m.List(f.ctx, f.rel)
	if err != nil {
		return nil, err
	}
	out := make([]fs.FileInfo, len(entries))
	for i, e := range entries {
		out[i] = entryInfo{e}
	}
	return out, nil
}

// ---- S3 读文件(*minio.Object 本身就是 ReadSeekCloser,Range 走
// 底层按需重新发起带偏移的 GET,不会整文件下载) ----

type s3ReadFile struct {
	obj  io.ReadSeekCloser
	info os.FileInfo
}

func (f *s3ReadFile) Close() error                 { return f.obj.Close() }
func (f *s3ReadFile) Read(p []byte) (int, error)   { return f.obj.Read(p) }
func (f *s3ReadFile) Seek(o int64, w int) (int64, error) { return f.obj.Seek(o, w) }
func (f *s3ReadFile) Write([]byte) (int, error)    { return 0, errPerm }
func (f *s3ReadFile) Readdir(int) ([]fs.FileInfo, error) { return nil, errPerm }
func (f *s3ReadFile) Stat() (os.FileInfo, error)   { return f.info, nil }

// ---- S3 写文件:pipe 流式中转,Close 时等待上传完成 ----

type s3WriteFile struct {
	rel  string
	pw   *io.PipeWriter
	done chan error
	n    int64
}

func newS3WriteFile(ctx context.Context, m *S3Mount, rel string) *s3WriteFile {
	pr, pw := io.Pipe()
	f := &s3WriteFile{rel: rel, pw: pw, done: make(chan error, 1)}
	go func() {
		err := m.Put(ctx, rel, pr, -1)
		// 上传失败时让写端尽快报错,而不是一直灌 pipe
		pr.CloseWithError(err)
		f.done <- err
	}()
	return f
}

func (f *s3WriteFile) Write(p []byte) (int, error) {
	n, err := f.pw.Write(p)
	f.n += int64(n)
	return n, err
}

func (f *s3WriteFile) Close() error {
	f.pw.Close()
	return <-f.done
}

func (f *s3WriteFile) Read([]byte) (int, error)          { return 0, errPerm }
func (f *s3WriteFile) Seek(int64, int) (int64, error)    { return 0, errPerm }
func (f *s3WriteFile) Readdir(int) ([]fs.FileInfo, error) { return nil, errPerm }
func (f *s3WriteFile) Stat() (os.FileInfo, error) {
	return entryInfo{Entry{Name: path.Base(f.rel), Size: f.n}}, nil
}
