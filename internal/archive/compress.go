package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"io"
	"path"
	"strings"
	"time"

	"pocketdrive/internal/db"
)

const (
	formatZip   = "zip"
	formatTarGz = "tar.gz"
	formatTarXz = "tar.xz"
	formatTar   = "tar"
)

// formatFromName 按扩展名判断格式(.tgz 等同 .tar.gz)。
func formatFromName(name string) string {
	l := strings.ToLower(name)
	switch {
	case strings.HasSuffix(l, ".zip"):
		return formatZip
	case strings.HasSuffix(l, ".tar.gz"), strings.HasSuffix(l, ".tgz"):
		return formatTarGz
	case strings.HasSuffix(l, ".tar.xz"), strings.HasSuffix(l, ".txz"):
		return formatTarXz
	case strings.HasSuffix(l, ".tar"):
		return formatTar
	}
	return ""
}

// countingWriter 统计写出的字节,用于压缩进度(按已读源字节更准,
// 但按写出字节能顺带反映压缩率,这里用已读源字节,见 runCompress)。
type progressReader struct {
	r    io.Reader
	n    *int64
	tick func()
}

func (p progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	*p.n += int64(n)
	p.tick()
	return n, err
}

// runCompress 把多个源打进一个包。全程流式:一次只有一个文件在内存里
// 过一遍缓冲区,不会因为包大就吃光内存。
func (s *Service) runCompress(ctx context.Context, t *db.ArchiveTask) error {
	srcs := strings.Split(t.Src, "|")
	destFS, destRel, err := s.resolve(t.Dest)
	if err != nil {
		return err
	}

	// 用管道把「写包」和「存包」接起来:边压边写进目标存储,
	// 中途不落一份完整的临时文件(S3 目标也一样)
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- destFS.create(ctx, destRel, pr, -1)
	}()

	err = s.writeArchive(ctx, t, srcs, pw)
	// 先关掉写端,让存储侧的 create 收到 EOF(或错误)后返回
	if err != nil {
		pw.CloseWithError(err)
		<-done
		return err
	}
	if err := pw.Close(); err != nil {
		<-done
		return err
	}
	return <-done
}

func (s *Service) writeArchive(ctx context.Context, t *db.ArchiveTask, srcs []string, w io.Writer) error {
	var written int64
	last := time.Now()
	tick := func() {
		// 进度写库限流,否则大文件会把 SQLite 刷爆
		if time.Since(last) > time.Second {
			last = time.Now()
			s.progress(t, written)
		}
	}

	if t.Format == formatZip {
		zw := zip.NewWriter(w)
		if err := s.eachFile(ctx, srcs, func(name string, e entry, rc io.ReadCloser) error {
			hdr := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: e.mtime}
			fw, err := zw.CreateHeader(hdr)
			if err != nil {
				return err
			}
			_, err = io.Copy(fw, progressReader{rc, &written, tick})
			return err
		}); err != nil {
			return err
		}
		return zw.Close()
	}

	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)
	if err := s.eachFile(ctx, srcs, func(name string, e entry, rc io.ReadCloser) error {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o644, Size: e.size,
			ModTime: e.mtime, Typeflag: tar.TypeReg,
		}); err != nil {
			return err
		}
		// tar 头里写死了 size,必须正好写这么多字节
		_, err := io.CopyN(tw, progressReader{rc, &written, tick}, e.size)
		return err
	}); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gw.Close()
}

// eachFile 按包内路径依次交出每个待压缩文件。多个源时以各自的名字
// 作为包内顶层目录,避免不同源的同名文件互相覆盖。
func (s *Service) eachFile(ctx context.Context, srcs []string,
	fn func(name string, e entry, rc io.ReadCloser) error) error {
	for _, src := range srcs {
		fsys, rel, err := s.resolve(src)
		if err != nil {
			return err
		}
		e, err := fsys.stat(ctx, rel)
		if err != nil {
			return err
		}
		base := path.Base(src)
		if !e.dir {
			rc, err := fsys.open(ctx, rel)
			if err != nil {
				return err
			}
			err = fn(base, e, rc)
			rc.Close()
			if err != nil {
				return err
			}
			continue
		}
		if err := fsys.walkFiles(ctx, rel, func(sub string, fe entry) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			rc, err := fsys.open(ctx, path.Join(rel, sub))
			if err != nil {
				return err
			}
			defer rc.Close()
			return fn(path.Join(base, sub), fe, rc)
		}); err != nil {
			return err
		}
	}
	return nil
}
