// Package cloud mounts S3-compatible storage (AWS S3 / Cloudflare R2 /
// MinIO…) as virtual folders "@Name" under the drive root.
//
// 架构(参考 AList/Cloudreve 的谱系,按小内存 VPS 取舍):
//   - 上传走服务端中转(浏览器只和本站通信,不存在任何 CORS 配置);
//     大文件分片映射为 S3 Multipart Upload,不在 VPS 落盘
//   - 下载/直链 302 到预签名 URL(R2 出站流量免费,不过 VPS 中转);
//     <video>/<img>/播放器加载媒体不受同源策略限制,同样无 CORS 问题
//   - WebDAV 通过同一套挂载分发(davfs.go)
//
// 边界(与本机策略的差异,均有意为之):离线下载/yt下载只能落本机
// (aria2/yt-dlp 是本机进程);外部存储删除不进回收站;不生成缩略图;
// 全局搜索不含外部存储;跨存储移动暂不支持。
package cloud

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"pocketdrive/internal/db"
	"pocketdrive/internal/httpx"
)

// MountPrefix 是挂载点在路径里的前缀:@Name/...
const MountPrefix = "@"

var nameRe = regexp.MustCompile(`^[\p{Han}A-Za-z0-9_-]{1,32}$`)

type Service struct {
	db *gorm.DB

	mu     sync.RWMutex
	mounts map[string]*S3Mount // name -> mount

	uMu   sync.Mutex
	usage map[string]usageEntry // name -> 用量缓存
}

func New(gdb *gorm.DB) *Service {
	s := &Service{
		db:     gdb,
		mounts: make(map[string]*S3Mount),
		usage:  make(map[string]usageEntry),
	}
	s.reload()
	return s
}

// policy 按挂载名取回策略(配额等元数据存在库里,不在 S3Mount 上)。
func (s *Service) policy(name string) (*db.StoragePolicy, error) {
	var p db.StoragePolicy
	if err := s.db.First(&p, "name = ?", name).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// reload 从 DB 重建挂载表(策略增删改后调用)。
func (s *Service) reload() {
	var policies []db.StoragePolicy
	s.db.Find(&policies)
	next := make(map[string]*S3Mount, len(policies))
	for i := range policies {
		m, err := newS3Mount(&policies[i])
		if err != nil {
			continue // 配置损坏的策略跳过,不拖垮其他挂载
		}
		next[policies[i].Name] = m
	}
	s.mu.Lock()
	s.mounts = next
	s.mu.Unlock()
}

// Names 返回全部挂载名(根目录虚拟文件夹用)。
func (s *Service) Names() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.mounts))
	for n := range s.mounts {
		out = append(out, n)
	}
	return out
}

// Resolve 判断相对路径是否落在某个挂载内:"@R2/a/b" → (mount, "a/b", true)。
// "@R2" 本身返回 rel=""。不是挂载路径返回 ok=false。
func (s *Service) Resolve(p string) (*S3Mount, string, bool) {
	if !strings.HasPrefix(p, MountPrefix) {
		return nil, "", false
	}
	name, rel, _ := strings.Cut(strings.TrimPrefix(p, MountPrefix), "/")
	s.mu.RLock()
	m := s.mounts[name]
	s.mu.RUnlock()
	if m == nil {
		return nil, "", false
	}
	return m, rel, true
}

// IsMountPath 报告路径是否以挂载前缀开头(即使挂载不存在,也应当被
// 当作"外部存储路径"对待,避免误落到本机)。
func IsMountPath(p string) bool { return strings.HasPrefix(p, MountPrefix) }

// ---- HTTP handlers(策略管理,登录后) ----

type policyView struct {
	ID        uint   `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	AccessKey string `json:"accessKey"`
	BasePath  string `json:"basePath"`
	Connected bool   `json:"connected"`
	// 容量:上限与当前用量(用量是缓存值,首次访问时后台统计)
	QuotaBytes   int64 `json:"quotaBytes"`
	UsedBytes    int64 `json:"usedBytes"`
	UsedFiles    int64 `json:"usedFiles"`
	UsagePending bool  `json:"usagePending"`
}

func (s *Service) HandleList(w http.ResponseWriter, r *http.Request) {
	var policies []db.StoragePolicy
	s.db.Order("id").Find(&policies)
	s.mu.RLock()
	connected := make(map[string]bool, len(policies))
	for _, p := range policies {
		connected[p.Name] = s.mounts[p.Name] != nil
	}
	s.mu.RUnlock()

	out := make([]policyView, len(policies))
	for i, p := range policies {
		v := policyView{
			ID: p.ID, Name: p.Name, Type: p.Type, Endpoint: p.Endpoint,
			Region: p.Region, Bucket: p.Bucket, AccessKey: p.AccessKey,
			BasePath: p.BasePath, Connected: connected[p.Name],
			QuotaBytes: p.QuotaBytes,
		}
		if v.Connected {
			u := s.Usage(p.Name)
			v.UsedBytes, v.UsedFiles, v.UsagePending = u.Bytes, u.Files, u.Pending
		}
		out[i] = v
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"policies": out})
}

type policyReq struct {
	ID        uint   `json:"id"`
	Name      string `json:"name"`
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
	BasePath  string `json:"basePath"`
	// QuotaGB 是前端填的容量上限(GB),0 或留空 = 不限
	QuotaGB float64 `json:"quotaGB"`
}

func (req *policyReq) normalize() error {
	req.Name = strings.TrimSpace(req.Name)
	req.Endpoint = strings.TrimSpace(req.Endpoint)
	req.Region = strings.TrimSpace(req.Region)
	req.Bucket = strings.TrimSpace(req.Bucket)
	req.AccessKey = strings.TrimSpace(req.AccessKey)
	req.SecretKey = strings.TrimSpace(req.SecretKey)
	req.BasePath = strings.Trim(strings.ReplaceAll(req.BasePath, "\\", "/"), "/ ")
	if !nameRe.MatchString(req.Name) {
		return errors.New("挂载名称只能是中文/字母/数字/下划线/横线,最长 32 字符")
	}
	if req.Endpoint == "" || req.Bucket == "" || req.AccessKey == "" {
		return errors.New("Endpoint、Bucket、AccessKey 均不能为空")
	}
	if !strings.HasPrefix(req.Endpoint, "http://") && !strings.HasPrefix(req.Endpoint, "https://") {
		req.Endpoint = "https://" + req.Endpoint
	}
	// Region 留空:R2 要求固定填 auto(它不支持 GetBucketLocation);其余
	// S3 兼容实现(AWS / MinIO…)留空交给 SDK 探测桶的真实 region——硬填
	// auto 会让 SigV4 签名校验失败(MinIO 报 "the region is wrong")
	if req.Region == "" && strings.Contains(req.Endpoint, ".r2.cloudflarestorage.com") {
		req.Region = "auto"
	}
	return nil
}

// HandleSave 创建或更新策略(id>0 更新;更新时 secretKey 留空 = 沿用原值)。
func (s *Service) HandleSave(w http.ResponseWriter, r *http.Request) {
	var req policyReq
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := req.normalize(); err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	var p db.StoragePolicy
	if req.ID > 0 {
		if err := s.db.First(&p, req.ID).Error; err != nil {
			httpx.Err(w, http.StatusNotFound, "策略不存在")
			return
		}
	} else if req.SecretKey == "" {
		httpx.Err(w, http.StatusBadRequest, "SecretKey 不能为空")
		return
	}
	// 名称冲突(排除自身)
	var cnt int64
	s.db.Model(&db.StoragePolicy{}).Where("name = ? AND id != ?", req.Name, req.ID).Count(&cnt)
	if cnt > 0 {
		httpx.Err(w, http.StatusBadRequest, "已存在同名挂载")
		return
	}
	p.Name, p.Type = req.Name, "s3"
	p.Endpoint, p.Region, p.Bucket = req.Endpoint, req.Region, req.Bucket
	p.AccessKey, p.BasePath = req.AccessKey, req.BasePath
	p.QuotaBytes = int64(req.QuotaGB * float64(1<<30))
	if p.QuotaBytes < 0 {
		p.QuotaBytes = 0
	}
	if req.SecretKey != "" {
		p.SecretKey = req.SecretKey
	}
	if err := s.db.Save(&p).Error; err != nil {
		httpx.Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.reload()
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "id": p.ID})
}

func (s *Service) HandleDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID uint `json:"id"`
	}
	if err := httpx.Decode(r, &req); err != nil || req.ID == 0 {
		httpx.Err(w, http.StatusBadRequest, "缺少 id")
		return
	}
	s.db.Delete(&db.StoragePolicy{}, req.ID)
	s.reload()
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// HandleTest 测试策略连通性:优先用请求体里的完整配置(表单里点测试,
// 还没保存也能测);只给 id 则测已保存的策略。逐层报错方便排查。
func (s *Service) HandleTest(w http.ResponseWriter, r *http.Request) {
	var req policyReq
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	p := db.StoragePolicy{
		Endpoint: req.Endpoint, Region: req.Region, Bucket: req.Bucket,
		AccessKey: req.AccessKey, SecretKey: req.SecretKey, BasePath: req.BasePath,
		Name: req.Name,
	}
	if req.ID > 0 {
		var saved db.StoragePolicy
		if err := s.db.First(&saved, req.ID).Error; err != nil {
			httpx.Err(w, http.StatusNotFound, "策略不存在")
			return
		}
		if p.SecretKey == "" {
			p.SecretKey = saved.SecretKey
		}
		if p.Endpoint == "" {
			p = saved
		}
	}
	if p.Name == "" {
		p.Name = "test"
	}
	req2 := policyReq{Name: p.Name, Endpoint: p.Endpoint, Region: p.Region,
		Bucket: p.Bucket, AccessKey: p.AccessKey, SecretKey: p.SecretKey, BasePath: p.BasePath}
	if err := req2.normalize(); err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	p.Endpoint, p.Region, p.BasePath = req2.Endpoint, req2.Region, req2.BasePath
	m, err := newS3Mount(&p)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "配置无效: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := m.Ping(ctx); err != nil {
		httpx.Err(w, http.StatusBadGateway, "连接失败: "+err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}
