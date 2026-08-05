package db

import (
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Setting struct {
	Key   string `gorm:"primaryKey;size:64"`
	Value string
}

type DownloadTask struct {
	GID             string    `gorm:"column:gid;primaryKey;size:32" json:"gid"`
	URL             string    `json:"url"`
	Dir             string    `json:"dir"`
	Name            string    `json:"name"`
	Status          string    `json:"status"`
	TotalLength     int64     `json:"totalLength"`
	CompletedLength int64     `json:"completedLength"`
	ErrorMsg        string    `json:"errorMsg"`
	// 磁力链:元数据下载完成后 aria2 会 follow 出新 gid,新记录记下旧 gid,
	// 前端拿着旧 gid 也能解析到当前任务
	Follows   string    `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"-"`
}

type Share struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	Token        string     `gorm:"uniqueIndex;size:32" json:"token"`
	Path         string     `json:"path"`
	Type         string     `json:"type"` // page(分享页) | direct(直链)
	PasswordHash string     `json:"-"`
	HasPassword  bool       `json:"hasPassword"`
	ExpiresAt    *time.Time `json:"expiresAt"`
	CreatedAt    time.Time  `json:"createdAt"`
}

type TrashItem struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	OrigPath  string    `json:"origPath"`
	Name      string    `json:"name"`
	TrashKey  string    `gorm:"uniqueIndex;size:64" json:"-"`
	Size      int64     `json:"size"`
	Dir       bool      `json:"dir"`
	DeletedAt time.Time `json:"deletedAt"`
}

// FolderIcon 记录文件夹的自定义 emoji 图标
type FolderIcon struct {
	Path string `gorm:"primaryKey;size:512" json:"path"`
	Icon string `gorm:"size:16" json:"icon"`
}

// StoragePolicy 外部存储策略(S3 兼容:AWS S3 / Cloudflare R2 / MinIO…),
// 挂载为网盘根目录下的 @Name 虚拟文件夹;SecretKey 永不回传前端。
type StoragePolicy struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	Name      string `gorm:"uniqueIndex;size:32" json:"name"`
	Type      string `gorm:"size:16" json:"type"` // s3
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"-"`
	BasePath  string `json:"basePath"` // 桶内前缀,可空
	// QuotaBytes 是这个挂载的容量上限,0 = 不限。软限制:用量靠定期
	// 遍历统计,刚写入的文件可能还没算进去。
	QuotaBytes int64     `json:"quotaBytes"`
	CreatedAt  time.Time `json:"createdAt"`
}

// UploadSession 是一次未完成的分片上传。存库(而不是只放内存)才能做到
// 断点续传:刷新页面、关标签页、甚至服务端重启后,重新选同一个文件都能
// 从断点继续。指纹 = sha256(目标路径|大小|文件修改时间|分片大小)。
type UploadSession struct {
	ID          string    `gorm:"primaryKey;size:40"`
	Fingerprint string    `gorm:"index;size:64"`
	Path        string    // 目标完整路径,外部存储含 @挂载名
	S3UploadID  string    // 仅外部存储:S3 Multipart 的 uploadId
	ChunkSize   int64     // 分片大小不同则分片边界不同,会话不可复用
	CreatedAt   time.Time `gorm:"index"`
}

// ArchiveTask 是一次压缩或解压。大包耗时长,做成异步任务:前端轮询
// 进度,刷新页面也不会丢。整盘导出走流式下载,不在这里记账。
type ArchiveTask struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	Kind     string `gorm:"size:16" json:"kind"`   // compress | extract
	Status   string `gorm:"size:16" json:"status"` // running | done | error
	Src      string `json:"src"`                   // 压缩:多个源以 | 分隔;解压:包路径
	Dest     string `json:"dest"`
	Format   string `gorm:"size:16" json:"format"`
	Total    int64  `json:"total"` // 总字节(压缩为源总大小,解压为包内声明大小)
	Done     int64  `json:"done"`
	ErrorMsg string `json:"errorMsg"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func Open(path string) (*gorm.DB, error) {
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	g, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}
	sqlDB, err := g.DB()
	if err != nil {
		return nil, err
	}
	// glebarez/sqlite 纯 Go 驱动,单连接避免写锁竞争
	sqlDB.SetMaxOpenConns(1)
	if err := g.AutoMigrate(&Setting{}, &DownloadTask{}, &Share{}, &TrashItem{}, &FolderIcon{}, &StoragePolicy{}, &UploadSession{}, &ArchiveTask{}); err != nil {
		return nil, err
	}
	return g, nil
}
