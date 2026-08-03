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
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"-"`
}

type YtdlpTask struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	URL       string    `json:"url"`
	Dir       string    `json:"dir"`
	Preset    string    `json:"preset"`
	Options   string    `json:"options"` // JSON:嵌入封面/元数据/字幕等开关
	Status    string    `json:"status"`
	Progress  float64   `json:"progress"`
	Title     string    `json:"title"`
	LogTail   string    `json:"logTail"`
	ErrorMsg  string    `json:"errorMsg"`
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
	if err := g.AutoMigrate(&Setting{}, &DownloadTask{}, &YtdlpTask{}, &Share{}, &TrashItem{}); err != nil {
		return nil, err
	}
	return g, nil
}
