package storage

import (
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
	"gorm.io/gorm"
)

const (
	recentN        = 8
	walkLimit      = 20000
	recentCacheTTL = 30 * time.Second
)

type RecentFile struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mtime"`
}

type Disk struct {
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Free        uint64  `json:"free"`
	UsedPercent float64 `json:"usedPercent"`
}

type Service struct {
	dataDir string
	fsys    fs.FS
	db      *gorm.DB

	mu       sync.Mutex
	cachedAt time.Time
	recent   []RecentFile

	// 本机用量(见 quota.go):遍历一次网盘目录得到,缓存 + 后台刷新
	uMu          sync.Mutex
	usageBytes   int64
	usageFiles   int64
	usageAt      time.Time
	usageRunning bool
}

func New(dataDir string, fsys fs.FS, gdb *gorm.DB) *Service {
	return &Service{dataDir: dataDir, fsys: fsys, db: gdb}
}

func (s *Service) DiskUsage() (*Disk, error) {
	u, err := disk.Usage(s.dataDir)
	if err != nil {
		return nil, err
	}
	return &Disk{Total: u.Total, Used: u.Used, Free: u.Free, UsedPercent: u.UsedPercent}, nil
}

// Recent returns the newest files under the data dir (cached; the walk
// is capped so a huge drive can't pin the CPU).
func (s *Service) Recent() []RecentFile {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Since(s.cachedAt) < recentCacheTTL {
		return s.recent
	}
	var out []RecentFile
	visited := 0
	_ = fs.WalkDir(s.fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		visited++
		if visited > walkLimit {
			return fs.SkipAll
		}
		base := path.Base(p)
		if strings.HasPrefix(base, ".") && p != "." {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out = append(out, RecentFile{
			Path:    p,
			Name:    base,
			Size:    info.Size(),
			ModTime: info.ModTime().UnixMilli(),
		})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime > out[j].ModTime })
	if len(out) > recentN {
		out = out[:recentN]
	}
	s.recent = out
	s.cachedAt = time.Now()
	return out
}
