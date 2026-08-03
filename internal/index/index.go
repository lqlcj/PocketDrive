// Package index maintains an in-memory file index for global search
// and category views (photos / music / videos). Filename search over a
// periodic directory walk — no FTS needed until content search lands.
package index

import (
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"pocketdrive/internal/httpx"
)

const (
	walkLimit = 50000
	cacheTTL  = 60 * time.Second
)

type Item struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Dir     bool   `json:"dir"`
	ModTime int64  `json:"mtime"`
	Kind    string `json:"kind"`
}

var extKind = map[string]string{
	".jpg": "image", ".jpeg": "image", ".png": "image", ".gif": "image",
	".webp": "image", ".svg": "image", ".bmp": "image", ".avif": "image",
	".mp4": "video", ".webm": "video", ".mkv": "video", ".mov": "video",
	".avi": "video", ".m4v": "video", ".flv": "video",
	".mp3": "audio", ".m4a": "audio", ".flac": "audio", ".wav": "audio",
	".ogg": "audio", ".aac": "audio", ".opus": "audio", ".wma": "audio",
	".md": "markdown",
}

func kindOf(name string) string {
	i := strings.LastIndex(name, ".")
	if i < 0 {
		return "other"
	}
	if k, ok := extKind[strings.ToLower(name[i:])]; ok {
		return k
	}
	return "other"
}

type Service struct {
	fsys fs.FS

	mu      sync.Mutex
	items   []Item
	builtAt time.Time
}

func New(fsys fs.FS) *Service {
	return &Service{fsys: fsys}
}

func (s *Service) snapshot() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.builtAt) < cacheTTL {
		return s.items
	}
	var out []Item
	visited := 0
	_ = fs.WalkDir(s.fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == "." {
			return nil
		}
		visited++
		if visited > walkLimit {
			return fs.SkipAll
		}
		if strings.HasPrefix(path.Base(p), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		it := Item{
			Path:    p,
			Name:    path.Base(p),
			Dir:     d.IsDir(),
			ModTime: info.ModTime().UnixMilli(),
		}
		if !d.IsDir() {
			it.Size = info.Size()
			it.Kind = kindOf(it.Name)
		}
		out = append(out, it)
		return nil
	})
	s.items = out
	s.builtAt = time.Now()
	return out
}

// Invalidate forces a rebuild on next query (call after big mutations).
func (s *Service) Invalidate() {
	s.mu.Lock()
	s.builtAt = time.Time{}
	s.mu.Unlock()
}

func (s *Service) Search(q string, limit int) []Item {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	var out []Item
	for _, it := range s.snapshot() {
		if strings.Contains(strings.ToLower(it.Name), q) {
			out = append(out, it)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

func (s *Service) Category(kind string, limit int) []Item {
	var out []Item
	for _, it := range s.snapshot() {
		if !it.Dir && it.Kind == kind {
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime > out[j].ModTime })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Stats returns file counts per kind for the island home page.
func (s *Service) Stats() map[string]int {
	stats := map[string]int{}
	for _, it := range s.snapshot() {
		if it.Dir {
			stats["folder"]++
		} else {
			stats[it.Kind]++
			stats["file"]++
		}
	}
	return stats
}

// ---- HTTP handlers ----

func (s *Service) HandleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	httpx.JSON(w, http.StatusOK, map[string]any{"results": s.Search(q, 50)})
}

func (s *Service) HandleCategory(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	switch kind {
	case "image", "audio", "video", "markdown":
	default:
		httpx.Err(w, http.StatusBadRequest, "未知分类")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"items": s.Category(kind, 500)})
}

func (s *Service) HandleStats(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{"stats": s.Stats()})
}
