// Package archive 提供在线压缩/解压与整盘导出导入。
//
// 压缩支持 zip 与 tar.gz;解压另外支持 tar.xz(xz 压缩太慢太吃内存,
// 在 2G VPS 上不划算,所以只解不压)。大包耗时长,压缩/解压都做成异步
// 任务,前端轮询进度——刷新页面不会中断。
//
// 全程流式:不把整个包读进内存,也不额外在磁盘上摊开一份。
package archive

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"pocketdrive/internal/auth"
	"pocketdrive/internal/cloud"
	"pocketdrive/internal/db"
	"pocketdrive/internal/files"
	"pocketdrive/internal/httpx"
	"pocketdrive/internal/logs"
)

const (
	// 解压安全阈值:压缩炸弹会用极小的包解出天量数据
	maxExtractBytes   = 200 << 30 // 单次解压最多 200GB
	maxExtractEntries = 200000    // 最多 20 万个条目
	// 同时只跑一个任务:2G VPS 上并发压缩会把内存和磁盘 IO 打满
	maxConcurrent = 1
)

type Service struct {
	db    *gorm.DB
	files *files.Service
	cloud *cloud.Service
	auth  *auth.Service

	dbPath     string // 整盘导出要把配置库一起打包
	appVersion string

	running atomic.Int32
}

func New(gdb *gorm.DB, fs *files.Service, cs *cloud.Service, as *auth.Service,
	dbPath, appVersion string) *Service {
	// 上一轮没跑完的任务在重启后不会自动恢复,标记为失败而不是永远转圈
	gdb.Model(&db.ArchiveTask{}).Where("status = ?", "running").
		Updates(map[string]any{"status": "error", "error_msg": "服务重启,任务中断"})
	return &Service{
		db: gdb, files: fs, cloud: cs, auth: as,
		dbPath: dbPath, appVersion: appVersion,
	}
}

// ---- 任务记账 ----

func (s *Service) progress(t *db.ArchiveTask, done int64) {
	t.Done = done
	// 每次都写库太吵,交给调用方按节奏调用
	s.db.Model(t).Updates(map[string]any{"done": done, "updated_at": time.Now()})
}

func (s *Service) finish(t *db.ArchiveTask, err error) {
	upd := map[string]any{"updated_at": time.Now()}
	if err != nil {
		upd["status"], upd["error_msg"] = "error", err.Error()
		logs.Errorf("archive", "任务 #%d(%s %s)失败: %v", t.ID, t.Kind, t.Src, err)
	} else {
		upd["status"], upd["done"] = "done", t.Total
	}
	s.db.Model(t).Updates(upd)
	s.running.Add(-1)
}

// start 起一个后台任务;同时只允许一个,避免小内存 VPS 被打满。
func (s *Service) start(t *db.ArchiveTask, run func(context.Context, *db.ArchiveTask) error) error {
	if s.running.Load() >= maxConcurrent {
		return errors.New("已有压缩/解压任务在进行,请等它完成")
	}
	if err := s.db.Create(t).Error; err != nil {
		return err
	}
	s.running.Add(1)
	go func() {
		// 不挂在请求上下文上:浏览器关掉了任务也要继续跑完
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Hour)
		defer cancel()
		s.finish(t, run(ctx, t))
	}()
	return nil
}

// ---- HTTP handlers ----

func (s *Service) HandleList(w http.ResponseWriter, r *http.Request) {
	var tasks []db.ArchiveTask
	s.db.Order("id DESC").Limit(50).Find(&tasks)
	httpx.JSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (s *Service) HandleDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID uint `json:"id"`
	}
	if err := httpx.Decode(r, &req); err != nil || req.ID == 0 {
		httpx.Err(w, http.StatusBadRequest, "缺少 id")
		return
	}
	// 只清记录,不动已经产出的文件
	s.db.Where("id = ? AND status <> ?", req.ID, "running").Delete(&db.ArchiveTask{})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) HandleCompress(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths  []string `json:"paths"`
		Dest   string   `json:"dest"`   // 目标包路径,含扩展名
		Format string   `json:"format"` // zip | tar.gz
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if len(req.Paths) == 0 {
		httpx.Err(w, http.StatusBadRequest, "没有选择要压缩的文件")
		return
	}
	format := req.Format
	if format == "" {
		format = formatFromName(req.Dest)
	}
	if format != formatZip && format != formatTarGz {
		httpx.Err(w, http.StatusBadRequest, "只支持压缩为 zip 或 tar.gz")
		return
	}
	dest := files.CleanPath(req.Dest)
	if dest == "" || strings.HasSuffix(dest, "/") {
		httpx.Err(w, http.StatusBadRequest, "请填写目标压缩包的文件名")
		return
	}

	srcs := make([]string, 0, len(req.Paths))
	for _, p := range req.Paths {
		c := files.CleanPath(p)
		if c == "" {
			httpx.Err(w, http.StatusBadRequest, "不能压缩根目录本身,请选中具体的文件或文件夹")
			return
		}
		if !sameStore(c, dest) {
			httpx.Err(w, http.StatusBadRequest, "压缩包要和源文件在同一个存储里")
			return
		}
		// 把包写进正在压缩的目录里会自己吃自己
		if dest == c || strings.HasPrefix(dest, c+"/") {
			httpx.Err(w, http.StatusBadRequest, "压缩包不能放在被压缩的文件夹内")
			return
		}
		srcs = append(srcs, c)
	}

	total, err := s.totalSize(r.Context(), srcs)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	t := &db.ArchiveTask{
		Kind: "compress", Status: "running", Format: format,
		Src: strings.Join(srcs, "|"), Dest: dest, Total: total,
	}
	if err := s.start(t, s.runCompress); err != nil {
		httpx.Err(w, http.StatusConflict, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "task": t})
}

func (s *Service) HandleExtract(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		Dest string `json:"dest"` // 解压到哪个目录,空 = 包所在目录
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	src := files.CleanPath(req.Path)
	if src == "" {
		httpx.Err(w, http.StatusBadRequest, "请选择压缩包")
		return
	}
	format := formatFromName(src)
	if format == "" {
		httpx.Err(w, http.StatusBadRequest, "无法识别的压缩格式,支持 zip / tar.gz / tar.xz / tar")
		return
	}
	dest := files.CleanPath(req.Dest)
	if req.Dest == "" {
		if dir := path.Dir(src); dir != "." {
			dest = dir
		} else {
			dest = ""
		}
	}
	if !sameStore(src, dest) {
		httpx.Err(w, http.StatusBadRequest, "只能解压到同一个存储里")
		return
	}

	fsys, rel, err := s.resolve(src)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	e, err := fsys.stat(r.Context(), rel)
	if err != nil || e.dir {
		httpx.Err(w, http.StatusNotFound, "压缩包不存在")
		return
	}

	t := &db.ArchiveTask{
		Kind: "extract", Status: "running", Format: format,
		Src: src, Dest: dest, Total: e.size,
	}
	if err := s.start(t, s.runExtract); err != nil {
		httpx.Err(w, http.StatusConflict, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "task": t})
}

// totalSize 统计待压缩内容的总字节,用于进度条分母。
func (s *Service) totalSize(ctx context.Context, srcs []string) (int64, error) {
	var total int64
	for _, src := range srcs {
		fsys, rel, err := s.resolve(src)
		if err != nil {
			return 0, err
		}
		e, err := fsys.stat(ctx, rel)
		if err != nil {
			return 0, fmt.Errorf("%s: 不存在", src)
		}
		if !e.dir {
			total += e.size
			continue
		}
		if err := fsys.walkFiles(ctx, rel, func(_ string, fe entry) error {
			total += fe.size
			return nil
		}); err != nil {
			return 0, err
		}
	}
	return total, nil
}
