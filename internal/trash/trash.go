// Package trash implements the recycle bin: deleting moves files into
// a hidden .trash directory inside the data dir (same volume, so it's
// an instant rename); items auto-purge after 30 days.
package trash

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"path"
	"time"

	"gorm.io/gorm"

	"pocketdrive/internal/cloud"
	"pocketdrive/internal/db"
	"pocketdrive/internal/files"
	"pocketdrive/internal/httpx"
)

const (
	trashDir  = ".trash"
	retention = 30 * 24 * time.Hour
)

type Service struct {
	db    *gorm.DB
	files *files.Service
	cloud *cloud.Service
	stop  chan struct{}
}

func New(gdb *gorm.DB, fs *files.Service, cs *cloud.Service) *Service {
	return &Service{db: gdb, files: fs, cloud: cs, stop: make(chan struct{})}
}

// Start runs the 30-day auto-purge loop.
func (s *Service) Start() {
	go func() {
		s.purgeExpired()
		t := time.NewTicker(6 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-t.C:
				s.purgeExpired()
			}
		}
	}()
}

func (s *Service) purgeExpired() {
	var items []db.TrashItem
	s.db.Where("deleted_at < ?", time.Now().Add(-retention)).Find(&items)
	for i := range items {
		_ = s.permDelete(&items[i])
	}
}

func randKey() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// Trash moves a path into the recycle bin.
func (s *Service) Trash(p string) error {
	p = files.CleanPath(p)
	if p == "" {
		return errors.New("不能删除根目录")
	}
	fi, err := s.files.Root().Stat(p)
	if err != nil {
		return errors.New("文件不存在: " + p)
	}
	if err := s.files.Root().MkdirAll(trashDir, 0o755); err != nil {
		return err
	}
	key := randKey()
	if err := s.files.Root().Rename(p, path.Join(trashDir, key)); err != nil {
		return err
	}
	item := db.TrashItem{
		OrigPath:  p,
		Name:      path.Base(p),
		TrashKey:  key,
		Size:      fi.Size(),
		Dir:       fi.IsDir(),
		DeletedAt: time.Now(),
	}
	if err := s.db.Create(&item).Error; err != nil {
		// DB 失败则把文件挪回去,避免"消失"
		_ = s.files.Root().Rename(path.Join(trashDir, key), p)
		return err
	}
	return nil
}

func (s *Service) restore(item *db.TrashItem) error {
	dest := item.OrigPath
	if dir := path.Dir(dest); dir != "." {
		if err := s.files.Root().MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if _, err := s.files.Root().Stat(dest); err == nil {
		return errors.New("原位置已有同名文件,请先处理后再还原")
	}
	if err := s.files.Root().Rename(path.Join(trashDir, item.TrashKey), dest); err != nil {
		return err
	}
	return s.db.Delete(&db.TrashItem{}, item.ID).Error
}

func (s *Service) permDelete(item *db.TrashItem) error {
	if err := s.files.Root().RemoveAll(path.Join(trashDir, item.TrashKey)); err != nil {
		return err
	}
	return s.db.Delete(&db.TrashItem{}, item.ID).Error
}

// ---- HTTP handlers ----

// HandleDeleteToTrash replaces the old permanent delete: files move to
// the recycle bin instead. 外部存储没有"同卷 rename",无法进回收站,
// 直接永久删除(前端弹窗有相应提示)。
func (s *Service) HandleDeleteToTrash(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths []string `json:"paths"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	for _, raw := range req.Paths {
		p := files.CleanPath(raw)
		if cloud.IsMountPath(p) {
			m, rel, ok := s.cloud.Resolve(p)
			if !ok {
				httpx.Err(w, http.StatusNotFound, "外部存储不存在或未挂载")
				return
			}
			if rel == "" {
				httpx.Err(w, http.StatusBadRequest, "挂载点请到「存储策略」里删除")
				return
			}
			if err := m.Delete(r.Context(), rel); err != nil {
				httpx.Err(w, http.StatusBadGateway, err.Error())
				return
			}
			continue
		}
		if err := s.Trash(p); err != nil {
			httpx.Err(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) HandleList(w http.ResponseWriter, r *http.Request) {
	var items []db.TrashItem
	s.db.Order("deleted_at DESC").Find(&items)
	httpx.JSON(w, http.StatusOK, map[string]any{"items": items, "retentionDays": 30})
}

func (s *Service) itemReq(w http.ResponseWriter, r *http.Request) (*db.TrashItem, bool) {
	var req struct {
		ID uint `json:"id"`
	}
	if err := httpx.Decode(r, &req); err != nil || req.ID == 0 {
		httpx.Err(w, http.StatusBadRequest, "缺少 id")
		return nil, false
	}
	var item db.TrashItem
	if err := s.db.First(&item, req.ID).Error; err != nil {
		httpx.Err(w, http.StatusNotFound, "回收站里没有这条记录")
		return nil, false
	}
	return &item, true
}

func (s *Service) HandleRestore(w http.ResponseWriter, r *http.Request) {
	item, ok := s.itemReq(w, r)
	if !ok {
		return
	}
	if err := s.restore(item); err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) HandlePermDelete(w http.ResponseWriter, r *http.Request) {
	item, ok := s.itemReq(w, r)
	if !ok {
		return
	}
	if err := s.permDelete(item); err != nil {
		httpx.Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) HandleEmpty(w http.ResponseWriter, r *http.Request) {
	var items []db.TrashItem
	s.db.Find(&items)
	for i := range items {
		if err := s.permDelete(&items[i]); err != nil {
			httpx.Err(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}
