package files

// 分片上传:大文件切块依次上传,网络波动只需重传失败的块。
// init → chunk×N → complete(顺序拼接落盘);暂存块超过 24h 自动清理。

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"pocketdrive/internal/httpx"
)

const (
	maxChunkSize = 32 << 20 // 单块上限 32MB
	maxChunks    = 4096
	tmpTTL       = 24 * time.Hour
)

var reUploadID = regexp.MustCompile(`^[0-9a-f]{32}$`)

// StartCleanup 定时清理超时未完成的分片暂存目录。
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
			time.Sleep(time.Hour)
		}
	}()
}

func (s *Service) HandleUploadInit(w http.ResponseWriter, r *http.Request) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		httpx.Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	id := hex.EncodeToString(b)
	if err := os.MkdirAll(filepath.Join(s.tmpDir, id), 0o755); err != nil {
		httpx.Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Service) HandleUploadChunk(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if !reUploadID.MatchString(id) {
		httpx.Err(w, http.StatusBadRequest, "无效的上传 id")
		return
	}
	index, err := strconv.Atoi(r.URL.Query().Get("index"))
	if err != nil || index < 0 || index >= maxChunks {
		httpx.Err(w, http.StatusBadRequest, "无效的分片序号")
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
	if !reUploadID.MatchString(req.ID) || req.Chunks <= 0 || req.Chunks > maxChunks {
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
