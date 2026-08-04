package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/ulikunitz/xz"

	"pocketdrive/internal/db"
)

// safeName 校验并规范化压缩包里的条目名。
//
// 压缩包是不可信输入:条目名可以是 "../../etc/passwd"、绝对路径、
// 或 Windows 盘符,不做处理就是经典的 Zip Slip 漏洞——解压能覆盖
// 数据目录外的任意文件。本机解压最终还会经 os.Root 兜底,但外部
// 存储没有这层保护,所以在这里统一挡掉。
func safeName(name string) (string, error) {
	n := strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(n, "/") {
		return "", fmt.Errorf("压缩包内含绝对路径条目: %s", name)
	}
	// C:/... 之类的盘符前缀
	if len(n) > 1 && n[1] == ':' {
		return "", fmt.Errorf("压缩包内含盘符路径条目: %s", name)
	}
	cleaned := path.Clean(n)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("压缩包内含越界路径条目: %s", name)
	}
	if cleaned == "." || cleaned == "/" {
		return "", nil // 忽略
	}
	return cleaned, nil
}

// extractGuard 盯住解压产出的总量,挡压缩炸弹(几十 KB 的包解出几百 GB)。
type extractGuard struct {
	bytes   int64
	entries int
}

func (g *extractGuard) add(n int64) error {
	g.bytes += n
	if g.bytes > maxExtractBytes {
		return errors.New("解压内容超过 200GB 上限,已中止(疑似压缩炸弹)")
	}
	return nil
}

func (g *extractGuard) entry() error {
	g.entries++
	if g.entries > maxExtractEntries {
		return errors.New("压缩包条目超过 20 万个,已中止(疑似压缩炸弹)")
	}
	return nil
}

func (s *Service) runExtract(ctx context.Context, t *db.ArchiveTask) error {
	srcFS, srcRel, err := s.resolve(t.Src)
	if err != nil {
		return err
	}
	destFS, destRel, err := s.resolve(t.Dest)
	if err != nil {
		return err
	}
	if err := destFS.mkdirAll(ctx, destRel); err != nil {
		return err
	}

	var read int64
	last := time.Now()
	tick := func() {
		if time.Since(last) > time.Second {
			last = time.Now()
			s.progress(t, read)
		}
	}
	guard := &extractGuard{}

	// 写一个条目:名字先过安全校验,再落到目标存储
	write := func(name string, size int64, r io.Reader) error {
		clean, err := safeName(name)
		if err != nil {
			return err
		}
		if clean == "" {
			return nil
		}
		if err := guard.entry(); err != nil {
			return err
		}
		if err := guard.add(size); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		return destFS.create(ctx, path.Join(destRel, clean), r, size)
	}

	if t.Format == formatZip {
		return s.extractZip(ctx, srcFS, srcRel, t, write, &read, tick)
	}

	rc, err := srcFS.open(ctx, srcRel)
	if err != nil {
		return err
	}
	defer rc.Close()
	counted := progressReader{rc, &read, tick}

	var stream io.Reader = counted
	switch t.Format {
	case formatTarGz:
		gr, err := gzip.NewReader(counted)
		if err != nil {
			return errors.New("不是有效的 gzip 包: " + err.Error())
		}
		defer gr.Close()
		stream = gr
	case formatTarXz:
		xr, err := xz.NewReader(counted)
		if err != nil {
			return errors.New("不是有效的 xz 包: " + err.Error())
		}
		stream = xr
	}

	tr := tar.NewReader(stream)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			clean, err := safeName(hdr.Name)
			if err != nil {
				return err
			}
			if clean != "" {
				if err := destFS.mkdirAll(ctx, path.Join(destRel, clean)); err != nil {
					return err
				}
			}
		case tar.TypeReg:
			if err := write(hdr.Name, hdr.Size, tr); err != nil {
				return err
			}
		default:
			// 符号链接/硬链接/设备文件一律跳过:链接可以指到数据目录外,
			// 是另一条绕过路径限制的通道
			continue
		}
	}
}

// extractZip 需要随机读(中央目录在文件末尾),所以要 ReaderAt。
// 外部存储上的包先取回本地临时文件,再按 zip 读——S3 对象没有可用的
// ReaderAt,硬来会退化成对每个条目重新发一次带 Range 的 GET。
func (s *Service) extractZip(ctx context.Context, srcFS vfs, srcRel string,
	t *db.ArchiveTask, write func(string, int64, io.Reader) error,
	read *int64, tick func()) error {

	ra, size, cleanup, err := s.readerAt(ctx, srcFS, srcRel, t.Total)
	if err != nil {
		return err
	}
	defer cleanup()

	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return errors.New("不是有效的 zip 包: " + err.Error())
	}
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "/") {
			clean, err := safeName(f.Name)
			if err != nil {
				return err
			}
			if clean != "" {
				continue // 目录由 create 自动建,不必单独处理
			}
			continue
		}
		// 条目声明的解压后大小同样可能是伪造的,写入时由 guard 兜底
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = write(f.Name, int64(f.UncompressedSize64), rc)
		rc.Close()
		if err != nil {
			return err
		}
		*read += int64(f.CompressedSize64)
		tick()
	}
	return nil
}

// readerAt 给 zip 提供随机读。本机文件直接用 *os.File;外部存储的包
// 先下载到临时文件。
func (s *Service) readerAt(ctx context.Context, fsys vfs, rel string, size int64) (io.ReaderAt, int64, func(), error) {
	rc, err := fsys.open(ctx, rel)
	if err != nil {
		return nil, 0, func() {}, err
	}
	if ra, ok := rc.(io.ReaderAt); ok {
		if f, isFile := rc.(*os.File); isFile {
			fi, err := f.Stat()
			if err != nil {
				rc.Close()
				return nil, 0, func() {}, err
			}
			return ra, fi.Size(), func() { rc.Close() }, nil
		}
	}
	defer rc.Close()
	tmp, err := os.CreateTemp("", "pd-zip-*")
	if err != nil {
		return nil, 0, func() {}, err
	}
	cleanup := func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}
	n, err := io.Copy(tmp, rc)
	if err != nil {
		cleanup()
		return nil, 0, func() {}, err
	}
	return tmp, n, cleanup, nil
}
