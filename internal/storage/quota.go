package storage

// 本机存储的用量与容量上限。
//
// 和外部存储那套(internal/cloud/usage.go)刻意保持同一个性格:
//   - 用量靠遍历目录得到,结果缓存、后台刷新,前台永不等;
//   - 上限是**软限制**,拿不到用量时一律放行——宁可让用户超一点,也不
//     能因为统计不出来把上传全堵死;
//   - 写成功后就地累加,不必等下一次全量统计。
//
// 比外部存储多一件事:本机还会真的把盘写满。磁盘剩余空间不够时同样
// 拦下来,报错要说人话,而不是让用户拿到一个 "no space left on device"。

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"pocketdrive/internal/db"
	"pocketdrive/internal/httpx"
)

const (
	usageTTL = 5 * time.Minute
	// 磁盘留白:低于这个数就不再收新文件。SQLite、缩略图、分片暂存都
	// 还要写盘,把盘顶死会连带把整个服务搞挂。
	diskReserve = 256 << 20
	quotaKey    = "local_quota_bytes"
)

// LocalUsage 是网盘目录的用量快照。
type LocalUsage struct {
	Bytes   int64 `json:"bytes"`
	Files   int64 `json:"files"`
	Quota   int64 `json:"quota"` // 0 = 不限
	Pending bool  `json:"pending"`
}

// Quota 读取设置里的容量上限,0 表示不限。
func (s *Service) Quota() int64 {
	if s.db == nil {
		return 0
	}
	var st db.Setting
	if s.db.First(&st, "key = ?", quotaKey).Error != nil {
		return 0
	}
	var v int64
	for _, c := range st.Value {
		if c < '0' || c > '9' {
			return 0
		}
		v = v*10 + int64(c-'0')
	}
	return v
}

// SetQuota 保存容量上限(字节),0 表示不限。
func (s *Service) SetQuota(bytes int64) error {
	if bytes < 0 {
		bytes = 0
	}
	return s.db.Save(&db.Setting{Key: quotaKey, Value: itoa(bytes)}).Error
}

// HandleSetQuota 设置页入口。quotaGB 以 GB 计,和 S3 挂载的填法保持一致;
// 0 或留空 = 不限。存的是字节,免得精度歧义。
func (s *Service) HandleSetQuota(w http.ResponseWriter, r *http.Request) {
	var req struct {
		QuotaGB float64 `json:"quotaGB"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	var bytes int64
	if req.QuotaGB > 0 {
		bytes = int64(req.QuotaGB * 1024 * 1024 * 1024)
	}
	if err := s.SetQuota(bytes); err != nil {
		httpx.Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "quota": bytes})
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

// Usage 返回网盘目录的用量。没统计过就先返回 Pending 并在后台跑,
// 设置页不该因为一次遍历卡住。
func (s *Service) Usage() LocalUsage {
	s.uMu.Lock()
	stale := time.Since(s.usageAt) > usageTTL
	never := s.usageAt.IsZero()
	if stale && !s.usageRunning {
		s.usageRunning = true
		go s.refreshUsage()
	}
	u := LocalUsage{Bytes: s.usageBytes, Files: s.usageFiles, Pending: never}
	s.uMu.Unlock()
	u.Quota = s.Quota()
	return u
}

func (s *Service) refreshUsage() {
	var bytes, files int64
	_ = fs.WalkDir(s.fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		// 回收站里的东西照样占盘,要算;但配置目录不在网盘里,本来就不会遍历到
		if d.IsDir() {
			return nil
		}
		if strings.HasPrefix(path.Base(p), ".") {
			return nil
		}
		if info, err := d.Info(); err == nil {
			bytes += info.Size()
			files++
		}
		return nil
	})
	s.uMu.Lock()
	s.usageBytes, s.usageFiles = bytes, files
	s.usageAt = time.Now()
	s.usageRunning = false
	s.uMu.Unlock()
}

// AddUsage 在写入成功后就地累加,免得配额检查要等下一轮全量统计。
func (s *Service) AddUsage(delta int64) {
	s.uMu.Lock()
	defer s.uMu.Unlock()
	if s.usageAt.IsZero() {
		return // 还没有基数,累加没有意义
	}
	s.usageBytes += delta
	if s.usageBytes < 0 {
		s.usageBytes = 0
	}
}

// UploadLimit returns the remaining configured local quota plus a small
// multipart framing allowance. Unknown usage deliberately returns zero so a
// background refresh cannot reject an otherwise valid upload.
func (s *Service) UploadLimit() int64 {
	q := s.Quota()
	if q <= 0 {
		return 0
	}
	s.uMu.Lock()
	defer s.uMu.Unlock()
	if s.usageAt.IsZero() {
		return 0
	}
	remaining := q - s.usageBytes
	if remaining <= 0 {
		return 1
	}
	return remaining
}

// FullError 是「装不下了」的统一错误,带一句能照着做的话。
type FullError struct{ msg string }

func (e *FullError) Error() string { return e.msg }

// CheckLocal 在写入本机存储之前检查。size 未知时传 0(只判断是否已经满了)。
func (s *Service) CheckLocal(size int64) error {
	if size < 0 {
		size = 0
	}
	// ① 用户设的上限
	if q := s.Quota(); q > 0 {
		s.uMu.Lock()
		used, known := s.usageBytes, !s.usageAt.IsZero()
		running := s.usageRunning
		if !known && !running {
			s.usageRunning = true
			go s.refreshUsage()
		}
		s.uMu.Unlock()
		// 统计不出来就放行:软限制
		if known && used+size > q {
			return &FullError{"本机存储已达到设置的容量上限(" +
				human(q) + "),请先清理或调大上限"}
		}
	}
	// ② 盘是不是真的快满了。这条不能软,写不进去就是写不进去
	du, err := s.DiskUsage()
	if err != nil {
		return nil
	}
	if int64(du.Free) < size+diskReserve {
		return &FullError{"服务器磁盘空间不足(剩余 " + human(int64(du.Free)) +
			"),请先清理回收站或删掉一些文件"}
	}
	return nil
}

// human 只给报错用,精度够看就行
func human(n int64) string {
	const unit = 1024
	if n < unit {
		return itoa(n) + " B"
	}
	units := []string{"KB", "MB", "GB", "TB"}
	v, i := float64(n), -1
	for v >= unit && i < len(units)-1 {
		v /= unit
		i++
	}
	whole := int64(v)
	frac := int64((v - float64(whole)) * 10)
	return itoa(whole) + "." + itoa(frac) + " " + units[i]
}
