package files

// 分片上传:大文件切块依次上传,网络波动只需重传失败的块。
// init(带目标路径) → chunk×N → complete。
// 本机存储:块暂存磁盘,complete 顺序拼接落盘;
// 外部存储:直接映射 S3 Multipart Upload(块即 Part,不在 VPS 落盘),
// 8MB 分片满足 S3 最小 5MB 分片要求。暂存/会话超过 24h 自动清理。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"

	"pocketdrive/internal/cloud"
	"pocketdrive/internal/httpx"
)

const (
	maxChunkSize = 32 << 20 // 单块上限 32MB
	maxChunks    = 4096
	tmpTTL       = 24 * time.Hour
)

var (
	reUploadID   = regexp.MustCompile(`^[0-9a-f]{32}$`)
	reS3UploadID = regexp.MustCompile(`^s3[0-9a-f]{32}$`)
)

// s3Upload 是一个进行中的外部存储分片会话。
type s3Upload struct {
	mount    *cloud.S3Mount
	rel      string
	uploadID string
	created  time.Time

	mu    sync.Mutex
	parts map[int]minio.CompletePart
}

type s3Uploads struct {
	mu sync.Mutex
	m  map[string]*s3Upload
}

func newS3Uploads() s3Uploads { return s3Uploads{m: make(map[string]*s3Upload)} }

func (u *s3Uploads) get(id string) *s3Upload {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.m[id]
}

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

// StartCleanup 定时清理超时未完成的分片暂存目录和 S3 会话。
func (s *Service) StartCleanup() {
	go func() {
		for {
			entries, err := os.ReadDir(s.tmpDir)
			if err == nil {
				for _, e := range entries {
					info, err := e.Info()
					if err == nil && time.Since(info.ModTime()) > tmpTTL {
						_ = os.RemoveAll(filepath.Join(s.tmpDir, e.Name()))
					}
				}
			}
			s.s3ups.mu.Lock()
			for id, up := range s.s3ups.m {
				if time.Since(up.created) > tmpTTL {
					ctx, cancel := contextTimeout()
					_ = up.mount.MultipartAbort(ctx, up.rel, up.uploadID)
					cancel()
					delete(s.s3ups.m, id)
				}
			}
			s.s3ups.mu.Unlock()
			time.Sleep(time.Hour)
		}
	}()
}

func (s *Service) HandleUploadInit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	// 兼容空 body(理论上前端总会带 path;不带就当本机)
	_ = httpx.Decode(r, &req)
	p := CleanPath(req.Path)

	if cloud.IsMountPath(p) {
		m, rel, bad := s.resolveMount(w, p)
		if bad {
			return
		}
		if rel == "" {
			httpx.Err(w, http.StatusBadRequest, "缺少目标文件路径")
			return
		}
		uploadID, err := m.MultipartInit(r.Context(), rel)
		if err != nil {
			httpx.Err(w, http.StatusBadGateway, "外部存储初始化分片失败: "+err.Error())
			return
		}
		id := "s3" + randHex()
		s.s3ups.mu.Lock()
		s.s3ups.m[id] = &s3Upload{
			mount: m, rel: rel, uploadID: uploadID,
			created: time.Now(), parts: make(map[int]minio.CompletePart),
		}
		s.s3ups.mu.Unlock()
		httpx.JSON(w, http.StatusOK, map[string]any{"id": id})
		return
	}

	id := randHex()
	if err := os.MkdirAll(filepath.Join(s.tmpDir, id), 0o755); err != nil {
		httpx.Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Service) HandleUploadChunk(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	index, err := strconv.Atoi(r.URL.Query().Get("index"))
	if err != nil || index < 0 || index >= maxChunks {
		httpx.Err(w, http.StatusBadRequest, "无效的分片序号")
		return
	}

	if reS3UploadID.MatchString(id) {
		up := s.s3ups.get(id)
		if up == nil {
			httpx.Err(w, http.StatusNotFound, "上传会话不存在或已过期")
			return
		}
		if r.ContentLength <= 0 || r.ContentLength > maxChunkSize {
			httpx.Err(w, http.StatusBadRequest, "分片大小无效")
			return
		}
		// S3 Part 序号从 1 开始
		part, err := up.mount.MultipartPut(r.Context(), up.rel, up.uploadID,
			index+1, io.LimitReader(r.Body, maxChunkSize), r.ContentLength)
		if err != nil {
			httpx.Err(w, http.StatusBadGateway, "分片上传到外部存储失败: "+err.Error())
			return
		}
		up.mu.Lock()
		up.parts[index] = part
		up.mu.Unlock()
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "size": r.ContentLength})
		return
	}

	if !reUploadID.MatchString(id) {
		httpx.Err(w, http.StatusBadRequest, "无效的上传 id")
		return
	}
	dir := filepath.Join(s.tmpDir, id)
	if _, err := os.Stat(dir); err != nil {
		httpx.Err(w, http.StatusNotFound, "上传会话不存在或已过期")
		return
	}
	tmp := filepath.Join(dir, fmt.Sprintf("part_%05d", index))
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

	if reS3UploadID.MatchString(req.ID) {
		up := s.s3ups.get(req.ID)
		if up == nil {
			httpx.Err(w, http.StatusNotFound, "上传会话不存在或已过期")
			return
		}
		up.mu.Lock()
		parts := make([]minio.CompletePart, 0, len(up.parts))
		for i := 0; i < req.Chunks; i++ {
			p, ok := up.parts[i]
			if !ok {
				up.mu.Unlock()
				httpx.Err(w, http.StatusBadRequest, fmt.Sprintf("缺少分片 %d,请重传", i))
				return
			}
			parts = append(parts, p)
		}
		up.mu.Unlock()
		sort.Slice(parts, func(i, j int) bool { return parts[i].PartNumber < parts[j].PartNumber })
		if err := up.mount.MultipartComplete(r.Context(), up.rel, up.uploadID, parts); err != nil {
			msg := "外部存储合并分片失败: " + err.Error()
			// 前端 CHUNK_SIZE 被调到 5MiB 以下时才会走到这;原始报错
			// ("EntityTooSmall")对用户毫无意义,翻成能照着改的提示
			if minio.ToErrorResponse(err).Code == "EntityTooSmall" {
				msg = "分片过小:S3 要求除最后一片外每片至少 5MiB,请调大前端 CHUNK_SIZE"
			}
			httpx.Err(w, http.StatusBadGateway, msg)
			return
		}
		s.s3ups.mu.Lock()
		delete(s.s3ups.m, req.ID)
		s.s3ups.mu.Unlock()
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	if !reUploadID.MatchString(req.ID) {
		httpx.Err(w, http.StatusBadRequest, "参数无效")
		return
	}
	p := CleanPath(req.Path)
	if p == "" {
		httpx.Err(w, http.StatusBadRequest, "路径不能为空")
		return
	}
	dir := filepath.Join(s.tmpDir, req.ID)
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
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}
