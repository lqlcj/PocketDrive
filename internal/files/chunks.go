package files

// 分片上传:大文件切块依次上传,网络波动只需重传失败的块。
// init(带目标路径+文件指纹) → chunk×N → complete。
//
// 本机存储:块暂存磁盘,complete 顺序拼接落盘;
// 外部存储:直接映射 S3 Multipart Upload(块即 Part,不在 VPS 落盘),
// 8MB 分片满足 S3 最小 5MB 分片要求。
//
// 断点续传:会话存库而非内存,init 时按文件指纹找回上次的会话并返回
// 已传分片列表。因此刷新页面、关标签页、甚至服务端重启后,重新选中
// 同一个文件都会接着上次的进度传。会话超过 24h 自动清理。

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/minio/minio-go/v7"

	"pocketdrive/internal/cloud"
	"pocketdrive/internal/db"
	"pocketdrive/internal/httpx"
)

const (
	maxChunkSize     = 32 << 20 // 单块上限 32MB
	defaultChunkSize = 8 << 20  // 前端没报分片大小时的假设值
	maxChunks        = 4096
	tmpTTL           = 24 * time.Hour
)

// 本机会话 32 位十六进制;外部存储会话多一个 s3 前缀
var reUploadID = regexp.MustCompile(`^(s3)?[0-9a-f]{32}$`)

// 已传完的分片文件名(半块是 part_00001.partial,不匹配)
var rePart = regexp.MustCompile(`^part_(\d{5})$`)

func randHex() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func contextTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// fingerprint 标识「同一个文件传到同一个位置」。文件被改过(大小或修改
// 时间变了)、或换了分片大小,指纹就不同,不会错误地接上旧进度。
func fingerprint(p string, size, lastModified, chunkSize int64) string {
	h := sha256.Sum256(fmt.Appendf(nil, "%s|%d|%d|%d", p, size, lastModified, chunkSize))
	return hex.EncodeToString(h[:])
}

func (s *Service) findSession(id string) (*db.UploadSession, bool) {
	if !reUploadID.MatchString(id) {
		return nil, false
	}
	var us db.UploadSession
	if s.db.First(&us, "id = ?", id).Error != nil {
		return nil, false
	}
	return &us, true
}

// mountOf 取出会话对应的外部存储挂载(本机会话返回 ok=false)。
func (s *Service) mountOf(us *db.UploadSession) (*cloud.S3Mount, string, bool) {
	if us.S3UploadID == "" {
		return nil, "", false
	}
	m, rel, ok := s.cloud.Resolve(us.Path)
	if !ok || rel == "" {
		return nil, "", false
	}
	return m, rel, true
}

// uploadedParts 返回已经传成功的分片序号(从 0 开始,升序)。
func (s *Service) uploadedParts(ctx context.Context, us *db.UploadSession) ([]int, error) {
	if us.S3UploadID != "" {
		m, rel, ok := s.mountOf(us)
		if !ok {
			return nil, errors.New("外部存储不存在或已卸载")
		}
		parts, err := m.MultipartUploaded(ctx, rel, us.S3UploadID)
		if err != nil {
			return nil, err
		}
		out := make([]int, 0, len(parts))
		for n := range parts {
			out = append(out, n-1) // S3 Part 序号从 1 开始
		}
		sort.Ints(out)
		return out, nil
	}
	entries, err := os.ReadDir(filepath.Join(s.tmpDir, us.ID))
	if err != nil {
		return nil, err
	}
	out := make([]int, 0, len(entries))
	for _, e := range entries {
		// 必须严格匹配:写了一半的块叫 part_00001.partial,不能算已传,
		// 否则续传会跳过它,最后拼出一个缺角的文件
		mm := rePart.FindStringSubmatch(e.Name())
		if mm == nil {
			continue
		}
		if n, err := strconv.Atoi(mm[1]); err == nil {
			out = append(out, n)
		}
	}
	sort.Ints(out)
	return out, nil
}

// dropSession 丢弃会话:外部存储要通知对端放弃(否则残片一直计费),
// 本机删暂存目录,最后清掉记录。
func (s *Service) dropSession(ctx context.Context, us *db.UploadSession) {
	if us.S3UploadID != "" {
		if m, rel, ok := s.mountOf(us); ok {
			_ = m.MultipartAbort(ctx, rel, us.S3UploadID)
		}
	} else {
		_ = os.RemoveAll(filepath.Join(s.tmpDir, us.ID))
	}
	s.db.Delete(&db.UploadSession{}, "id = ?", us.ID)
}

// StartCleanup 定时清理超时未完成的上传会话。
func (s *Service) StartCleanup() {
	go func() {
		for {
			var stale []db.UploadSession
			s.db.Where("created_at < ?", time.Now().Add(-tmpTTL)).Find(&stale)
			for i := range stale {
				ctx, cancel := contextTimeout()
				s.dropSession(ctx, &stale[i])
				cancel()
			}
			// 没有会话记录的孤儿暂存目录(如断电导致记录丢失)一并清掉
			if entries, err := os.ReadDir(s.tmpDir); err == nil {
				for _, e := range entries {
					info, err := e.Info()
					if err != nil || time.Since(info.ModTime()) <= tmpTTL {
						continue
					}
					var cnt int64
					s.db.Model(&db.UploadSession{}).Where("id = ?", e.Name()).Count(&cnt)
					if cnt == 0 {
						_ = os.RemoveAll(filepath.Join(s.tmpDir, e.Name()))
					}
				}
			}
			time.Sleep(time.Hour)
		}
	}()
}

func (s *Service) HandleUploadInit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		// 下面三项用于识别「同一次上传」以支持断点续传;老前端不带也能工作,
		// 只是每次都从头传
		Size         int64 `json:"size"`
		LastModified int64 `json:"lastModified"`
		ChunkSize    int64 `json:"chunkSize"`
	}
	_ = httpx.Decode(r, &req)
	p := CleanPath(req.Path)
	if p == "" {
		httpx.Err(w, http.StatusBadRequest, "缺少目标文件路径")
		return
	}
	if req.ChunkSize <= 0 || req.ChunkSize > maxChunkSize {
		req.ChunkSize = defaultChunkSize
	}

	// 有指纹才谈得上续传:找回未过期的旧会话,把已传分片告诉前端
	if req.Size > 0 {
		fp := fingerprint(p, req.Size, req.LastModified, req.ChunkSize)
		var old db.UploadSession
		if s.db.First(&old, "fingerprint = ?", fp).Error == nil {
			if time.Since(old.CreatedAt) < tmpTTL {
				if uploaded, err := s.uploadedParts(r.Context(), &old); err == nil {
					httpx.JSON(w, http.StatusOK, map[string]any{
						"id": old.ID, "uploaded": uploaded, "chunkSize": old.ChunkSize,
					})
					return
				}
			}
			// 过期,或对端已经把这次分片上传丢弃了 → 清掉重来
			s.dropSession(r.Context(), &old)
		}
	}

	us := db.UploadSession{Path: p, ChunkSize: req.ChunkSize, CreatedAt: time.Now()}
	if req.Size > 0 {
		us.Fingerprint = fingerprint(p, req.Size, req.LastModified, req.ChunkSize)
	}

	if cloud.IsMountPath(p) {
		m, rel, bad := s.resolveMount(w, p)
		if bad {
			return
		}
		if rel == "" {
			httpx.Err(w, http.StatusBadRequest, "缺少目标文件路径")
			return
		}
		// 分片上传的总大小是已知的,能提前把超配额的文件挡在门外
		if err := s.cloud.CheckQuota(m.Name, req.Size); err != nil {
			httpx.Err(w, http.StatusInsufficientStorage, err.Error())
			return
		}
		uploadID, err := m.MultipartInit(r.Context(), rel)
		if err != nil {
			httpx.Err(w, http.StatusBadGateway, "外部存储初始化分片失败: "+err.Error())
			return
		}
		us.ID, us.S3UploadID = "s3"+randHex(), uploadID
	} else {
		us.ID = randHex()
		if err := os.MkdirAll(filepath.Join(s.tmpDir, us.ID), 0o755); err != nil {
			httpx.Err(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := s.db.Create(&us).Error; err != nil {
		httpx.Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"id": us.ID, "uploaded": []int{}, "chunkSize": us.ChunkSize,
	})
}

func (s *Service) HandleUploadChunk(w http.ResponseWriter, r *http.Request) {
	index, err := strconv.Atoi(r.URL.Query().Get("index"))
	if err != nil || index < 0 || index >= maxChunks {
		httpx.Err(w, http.StatusBadRequest, "无效的分片序号")
		return
	}
	us, ok := s.findSession(r.URL.Query().Get("id"))
	if !ok {
		httpx.Err(w, http.StatusNotFound, "上传会话不存在或已过期")
		return
	}

	if us.S3UploadID != "" {
		m, rel, ok := s.mountOf(us)
		if !ok {
			httpx.Err(w, http.StatusNotFound, "外部存储不存在或已卸载")
			return
		}
		if r.ContentLength <= 0 || r.ContentLength > maxChunkSize {
			httpx.Err(w, http.StatusBadRequest, "分片大小无效")
			return
		}
		// S3 Part 序号从 1 开始;ETag 由 ListParts 在合并时取回,这里不必缓存
		if _, err := m.MultipartPut(r.Context(), rel, us.S3UploadID,
			index+1, io.LimitReader(r.Body, maxChunkSize), r.ContentLength); err != nil {
			httpx.Err(w, http.StatusBadGateway, "分片上传到外部存储失败: "+err.Error())
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "size": r.ContentLength})
		return
	}

	dir := filepath.Join(s.tmpDir, us.ID)
	if _, err := os.Stat(dir); err != nil {
		httpx.Err(w, http.StatusNotFound, "上传会话不存在或已过期")
		return
	}
	// 先写临时名再改名:中断的块不会被误当成已传完的块跳过
	part := filepath.Join(dir, fmt.Sprintf("part_%05d", index))
	tmp := part + ".partial"
	f, err := os.Create(tmp)
	if err != nil {
		httpx.Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	n, err := io.Copy(f, io.LimitReader(r.Body, maxChunkSize+1))
	f.Close()
	if err != nil || n > maxChunkSize {
		os.Remove(tmp)
		httpx.Err(w, http.StatusBadRequest, "分片写入失败或超过 32MB")
		return
	}
	if err := os.Rename(tmp, part); err != nil {
		os.Remove(tmp)
		httpx.Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "size": n})
}

func (s *Service) HandleUploadComplete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     string `json:"id"`
		Path   string `json:"path"`
		Chunks int    `json:"chunks"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.Chunks <= 0 || req.Chunks > maxChunks {
		httpx.Err(w, http.StatusBadRequest, "参数无效")
		return
	}
	us, ok := s.findSession(req.ID)
	if !ok {
		httpx.Err(w, http.StatusNotFound, "上传会话不存在或已过期")
		return
	}
	// 目标路径以会话里记录的为准,客户端传的仅作兼容
	p := us.Path

	if us.S3UploadID != "" {
		m, rel, ok := s.mountOf(us)
		if !ok {
			httpx.Err(w, http.StatusNotFound, "外部存储不存在或已卸载")
			return
		}
		uploaded, err := m.MultipartUploaded(r.Context(), rel, us.S3UploadID)
		if err != nil {
			httpx.Err(w, http.StatusBadGateway, "读取已传分片失败: "+err.Error())
			return
		}
		parts := make([]minio.CompletePart, 0, req.Chunks)
		for i := 0; i < req.Chunks; i++ {
			p, ok := uploaded[i+1]
			if !ok {
				httpx.Err(w, http.StatusBadRequest, fmt.Sprintf("缺少分片 %d,请重传", i))
				return
			}
			parts = append(parts, p)
		}
		sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })
		if err := m.MultipartComplete(r.Context(), rel, us.S3UploadID, parts); err != nil {
			msg := "外部存储合并分片失败: " + err.Error()
			// 前端 CHUNK_SIZE 被调到 5MiB 以下时才会走到这;原始报错
			// ("EntityTooSmall")对用户毫无意义,翻成能照着改的提示
			if minio.ToErrorResponse(err).Code == "EntityTooSmall" {
				msg = "分片过小:S3 要求除最后一片外每片至少 5MiB,请调大前端 CHUNK_SIZE"
			}
			httpx.Err(w, http.StatusBadGateway, msg)
			return
		}
		s.db.Delete(&db.UploadSession{}, "id = ?", us.ID)
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	dir := filepath.Join(s.tmpDir, us.ID)
	// 先校验所有分片齐全
	for i := 0; i < req.Chunks; i++ {
		if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("part_%05d", i))); err != nil {
			httpx.Err(w, http.StatusBadRequest, fmt.Sprintf("缺少分片 %d,请重传", i))
			return
		}
	}
	if parent := path.Dir(p); parent != "." {
		if err := s.root.MkdirAll(parent, 0o755); err != nil {
			httpx.Err(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	out, err := s.root.Create(p)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	for i := 0; i < req.Chunks; i++ {
		part, err := os.Open(filepath.Join(dir, fmt.Sprintf("part_%05d", i)))
		if err == nil {
			_, err = io.Copy(out, part)
			part.Close()
		}
		if err != nil {
			out.Close()
			_ = s.root.Remove(p)
			httpx.Err(w, http.StatusInternalServerError, "拼接分片失败: "+err.Error())
			return
		}
	}
	if err := out.Close(); err != nil {
		httpx.Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = os.RemoveAll(dir)
	s.db.Delete(&db.UploadSession{}, "id = ?", us.ID)
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}
