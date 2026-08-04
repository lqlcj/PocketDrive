package cloud

// 外部存储的用量统计与配额。
//
// S3 没有便宜的「桶有多大」接口,只能把对象列一遍。大桶列一次要几秒到
// 几十秒,所以结果进缓存、后台刷新,前台永远不等。
//
// 配额因此是软限制:刚传完的文件可能还没算进用量,超出一点点才被拦下。
// 对个人网盘来说这个精度足够——它防的是「不知不觉把桶塞爆」,不是计费。

import (
	"context"
	"sync"
	"time"
)

const (
	usageTTL     = 10 * time.Minute
	usageTimeout = 2 * time.Minute
)

// Usage 是一个挂载的用量快照。
type Usage struct {
	Name    string    `json:"name"`
	Bytes   int64     `json:"bytes"`
	Files   int64     `json:"files"`
	Quota   int64     `json:"quota"` // 0 = 不限
	At      time.Time `json:"at"`
	Pending bool      `json:"pending"` // 还没统计过,正在后台跑
}

type usageEntry struct {
	bytes, files int64
	at           time.Time
	running      bool
}

// Usage 返回某个挂载的用量。没有缓存时立刻返回 Pending 并在后台开始
// 统计——不能让设置页因为一个大桶卡住。
func (s *Service) Usage(name string) Usage {
	s.uMu.Lock()
	e, ok := s.usage[name]
	stale := !ok || time.Since(e.at) > usageTTL
	if stale && !e.running {
		e.running = true
		s.usage[name] = e
		go s.refreshUsage(name)
	}
	s.uMu.Unlock()

	u := Usage{Name: name, Bytes: e.bytes, Files: e.files, At: e.at, Pending: !ok}
	if p, err := s.policy(name); err == nil {
		u.Quota = p.QuotaBytes
	}
	return u
}

func (s *Service) refreshUsage(name string) {
	defer func() {
		s.uMu.Lock()
		e := s.usage[name]
		e.running = false
		s.usage[name] = e
		s.uMu.Unlock()
	}()

	s.mu.RLock()
	m := s.mounts[name]
	s.mu.RUnlock()
	if m == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), usageTimeout)
	defer cancel()

	var bytes, files int64
	if err := m.WalkFiles(ctx, "", func(_ string, size int64, _ time.Time) error {
		bytes += size
		files++
		return nil
	}); err != nil {
		return // 统计失败就保留上一次的结果,不要把用量清零
	}
	s.uMu.Lock()
	s.usage[name] = usageEntry{bytes: bytes, files: files, at: time.Now(), running: true}
	s.uMu.Unlock()
}

// AddUsage 在写入成功后就地累加,让配额检查不必等下一次全量统计。
// 下一次统计会用真实值覆盖它。
func (s *Service) AddUsage(name string, delta int64) {
	s.uMu.Lock()
	defer s.uMu.Unlock()
	e, ok := s.usage[name]
	if !ok {
		return // 还没统计过基数,累加没有意义
	}
	e.bytes += delta
	if e.bytes < 0 {
		e.bytes = 0
	}
	s.usage[name] = e
}

// CheckQuota 在写入前检查配额。size 未知时传 0(只判断是否已经超了)。
//
// 拿不到用量(从没统计过、或统计失败)时一律放行:宁可让用户超一点,
// 也不能因为统计不出来就把上传全堵死。
func (s *Service) CheckQuota(name string, size int64) error {
	p, err := s.policy(name)
	if err != nil || p.QuotaBytes <= 0 {
		return nil
	}
	s.uMu.Lock()
	e, ok := s.usage[name]
	s.uMu.Unlock()
	if !ok || e.at.IsZero() {
		go func() {
			s.uMu.Lock()
			cur := s.usage[name]
			if !cur.running {
				cur.running = true
				s.usage[name] = cur
				s.uMu.Unlock()
				s.refreshUsage(name)
				return
			}
			s.uMu.Unlock()
		}()
		return nil
	}
	if e.bytes+size > p.QuotaBytes {
		return &QuotaError{Name: name, Used: e.bytes, Quota: p.QuotaBytes}
	}
	return nil
}

// QuotaError 让调用方能识别「是配额问题」而不是别的失败。
type QuotaError struct {
	Name  string
	Used  int64
	Quota int64
}

func (e *QuotaError) Error() string {
	return "外部存储 @" + e.Name + " 已达到设置的容量上限,请先清理或调大上限"
}

// usageSnapshot 供容量页一次拿全部挂载的用量。
func (s *Service) UsageAll() []Usage {
	names := s.Names()
	out := make([]Usage, 0, len(names))
	for _, n := range names {
		out = append(out, s.Usage(n))
	}
	return out
}

var _ = sync.Mutex{}
