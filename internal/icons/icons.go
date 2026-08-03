// Package icons stores per-folder emoji icons.
package icons

import (
	"net/http"

	"gorm.io/gorm"

	"pocketdrive/internal/db"
	"pocketdrive/internal/files"
	"pocketdrive/internal/httpx"
)

type Service struct {
	db *gorm.DB
}

func New(gdb *gorm.DB) *Service { return &Service{db: gdb} }

func (s *Service) HandleList(w http.ResponseWriter, r *http.Request) {
	var rows []db.FolderIcon
	s.db.Find(&rows)
	m := make(map[string]string, len(rows))
	for _, row := range rows {
		m[row.Path] = row.Icon
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"icons": m})
}

// HandleSet sets or clears (icon="") a folder's icon.
func (s *Service) HandleSet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		Icon string `json:"icon"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	p := files.CleanPath(req.Path)
	if p == "" || len(p) > 512 || len(req.Icon) > 16 {
		httpx.Err(w, http.StatusBadRequest, "参数无效")
		return
	}
	if req.Icon == "" {
		s.db.Delete(&db.FolderIcon{}, "path = ?", p)
	} else {
		s.db.Save(&db.FolderIcon{Path: p, Icon: req.Icon})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}
