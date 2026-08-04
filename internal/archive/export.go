package archive

// 整盘导出/导入:换 VPS 时把网盘整个搬走。
//
// 导出是流式 tar.gz,直接推给浏览器——不在服务器上先摊一份完整的包,
// 否则磁盘剩余空间不够时根本导不出来。
//
// 包内结构:
//
//	manifest.json     版本、导出时间、文件数,导入时先校验它
//	pocketdrive.db    配置库(分享链接/下载历史/文件夹图标/存储策略)
//	data/...          网盘文件
//
// 外部存储(@挂载)的内容不打包:对象存储本身不需要跟着迁,导入后按原
// 策略重新挂上即可(密钥在配置库里)。

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pocketdrive/internal/httpx"
)

// manifestVersion 是包格式版本。导入时只接受同一个大版本,免得旧包
// 恢复到新结构上得到一个半坏的库。
const manifestVersion = 1

const manifestName = "manifest.json"

// importedSuffix:导入的配置库先写在旁边,下次启动才顶替正式库——
// 运行中的进程正握着旧文件的句柄,当场替换在 Windows 上直接失败,
// 在 Linux 上则会让后续写入落到已被删除的 inode 上。
const importedSuffix = ".imported"

type manifest struct {
	Version    int       `json:"version"`
	App        string    `json:"app"`
	AppVersion string    `json:"appVersion"`
	ExportedAt time.Time `json:"exportedAt"`
	Files      int       `json:"files"`
	Bytes      int64     `json:"bytes"`
}

// HandleExport 把 data 目录和配置库打成一个 tar.gz 流式返回。
func (s *Service) HandleExport(w http.ResponseWriter, r *http.Request) {
	// 配置库不能边写边读:先 VACUUM 出一份一致的快照(顺带压实)
	snap, err := s.snapshotDB()
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, "导出配置库失败: "+err.Error())
		return
	}
	defer os.Remove(snap)

	name := fmt.Sprintf("pocketdrive-%s.tar.gz", time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition",
		"attachment; filename*=UTF-8''"+url.PathEscape(name))

	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)

	// manifest 先写:导入时读到头几个字节就能判断版本
	root := s.files.Root()
	var count int
	var bytes int64
	_ = fs.WalkDir(root.FS(), ".", func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if fi, err := d.Info(); err == nil {
			count++
			bytes += fi.Size()
		}
		return nil
	})
	mf, _ := json.MarshalIndent(manifest{
		Version: manifestVersion, App: "PocketDrive", AppVersion: s.appVersion,
		ExportedAt: time.Now(), Files: count, Bytes: bytes,
	}, "", "  ")
	if err := writeTarBytes(tw, manifestName, mf); err != nil {
		return // 客户端断了,没什么可做的
	}

	if snapData, err := os.Open(snap); err == nil {
		if fi, err := snapData.Stat(); err == nil {
			// filepath.Base 而非 path.Base:dbPath 是本机路径,Windows 上
			// 用斜杠语义取不出文件名,整条 C:\... 会被当成包内条目名
			_ = tw.WriteHeader(&tar.Header{
				Name: filepath.Base(s.dbPath), Mode: 0o600,
				Size: fi.Size(), ModTime: time.Now(), Typeflag: tar.TypeReg,
			})
			_, _ = io.Copy(tw, snapData)
		}
		snapData.Close()
	}

	// 网盘文件:整个流式过一遍,不额外占磁盘
	_ = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		f, err := root.Open(name)
		if err != nil {
			return nil
		}
		defer f.Close()
		if err := tw.WriteHeader(&tar.Header{
			Name: "data/" + name, Mode: 0o644, Size: fi.Size(),
			ModTime: fi.ModTime(), Typeflag: tar.TypeReg,
		}); err != nil {
			return err
		}
		_, err = io.CopyN(tw, f, fi.Size())
		return err
	})

	_ = tw.Close()
	_ = gw.Close()
}

func writeTarBytes(tw *tar.Writer, name string, b []byte) error {
	if err := tw.WriteHeader(&tar.Header{
		Name: name, Mode: 0o644, Size: int64(len(b)),
		ModTime: time.Now(), Typeflag: tar.TypeReg,
	}); err != nil {
		return err
	}
	_, err := tw.Write(b)
	return err
}

// snapshotDB 用 VACUUM INTO 导出一份一致的库快照。
func (s *Service) snapshotDB() (string, error) {
	tmp, err := os.CreateTemp("", "pd-db-*.db")
	if err != nil {
		return "", err
	}
	p := tmp.Name()
	tmp.Close()
	// VACUUM INTO 要求目标文件不存在
	if err := os.Remove(p); err != nil {
		return "", err
	}
	if err := s.db.Exec("VACUUM INTO ?", p).Error; err != nil {
		os.Remove(p)
		return "", err
	}
	return p, nil
}

// HandleImport 从上传的包恢复。这会覆盖现有网盘内容,必须先验密码。
func (s *Service) HandleImport(w http.ResponseWriter, r *http.Request) {
	if !s.auth.Check(s.auth.User(), r.Header.Get("X-PD-Password")) {
		httpx.Err(w, http.StatusForbidden, "密码不正确,导入已取消")
		return
	}
	gr, err := gzip.NewReader(r.Body)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "不是有效的 tar.gz 备份包")
		return
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var mf *manifest
	var restored int
	dbTarget := s.dbPath + importedSuffix
	var dbWritten bool
	guard := &extractGuard{}

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			httpx.Err(w, http.StatusBadRequest, "读取备份包失败: "+err.Error())
			return
		}
		if hdr.Typeflag != tar.TypeReg {
			continue // 目录会按需创建,链接一律跳过
		}
		clean, err := safeName(hdr.Name)
		if err != nil {
			httpx.Err(w, http.StatusBadRequest, err.Error())
			return
		}
		if clean == "" {
			continue
		}

		switch {
		case clean == manifestName:
			var m manifest
			if err := json.NewDecoder(io.LimitReader(tr, 1<<20)).Decode(&m); err != nil {
				httpx.Err(w, http.StatusBadRequest, "备份包的 manifest 损坏")
				return
			}
			if m.Version != manifestVersion {
				httpx.Err(w, http.StatusBadRequest, fmt.Sprintf(
					"备份包版本 %d 与当前程序(%d)不兼容", m.Version, manifestVersion))
				return
			}
			mf = &m

		case clean == filepath.Base(s.dbPath):
			// manifest 必须排在库前面,否则无法先做版本校验
			if mf == nil {
				httpx.Err(w, http.StatusBadRequest, "备份包结构异常:缺少 manifest")
				return
			}
			if err := writeLocalFile(dbTarget, tr, hdr.Size); err != nil {
				httpx.Err(w, http.StatusInternalServerError, "写入配置库失败: "+err.Error())
				return
			}
			dbWritten = true

		case strings.HasPrefix(clean, "data/"):
			if mf == nil {
				httpx.Err(w, http.StatusBadRequest, "备份包结构异常:缺少 manifest")
				return
			}
			if err := guard.entry(); err != nil {
				httpx.Err(w, http.StatusBadRequest, err.Error())
				return
			}
			if err := guard.add(hdr.Size); err != nil {
				httpx.Err(w, http.StatusBadRequest, err.Error())
				return
			}
			rel := strings.TrimPrefix(clean, "data/")
			if rel == "" {
				continue
			}
			// 经 os.Root 写入,越界路径在这里也会被再挡一次
			if err := (localFS{s.files.Root()}).create(r.Context(), rel, tr, hdr.Size); err != nil {
				httpx.Err(w, http.StatusInternalServerError,
					fmt.Sprintf("恢复 %s 失败: %v", rel, err))
				return
			}
			restored++
		}
	}

	if mf == nil {
		httpx.Err(w, http.StatusBadRequest, "这不像是 PocketDrive 的备份包(没有 manifest)")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"ok": true, "files": restored, "database": dbWritten,
		"exportedAt": mf.ExportedAt,
		"note":       "网盘文件已就位;配置库将在重启后生效,请重启容器",
	})
}

func writeLocalFile(p string, r io.Reader, size int64) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.CopyN(f, r, size); err != nil {
		f.Close()
		os.Remove(p)
		return err
	}
	return f.Close()
}

// RestorePendingImport 在启动时(打开数据库之前)顶替导入进来的配置库。
// 没有待恢复的库时什么都不做。
func RestorePendingImport(dbPath string) error {
	pending := dbPath + importedSuffix
	if _, err := os.Stat(pending); err != nil {
		return nil
	}
	// WAL/SHM 属于旧库,留着会和新库对不上
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Remove(dbPath + suffix)
	}
	if err := os.Rename(pending, dbPath); err != nil {
		return fmt.Errorf("顶替导入的配置库失败: %w", err)
	}
	return nil
}
