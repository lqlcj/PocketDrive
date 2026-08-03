package files

import (
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"

	"pocketdrive/internal/httpx"
)

const maxTextPreview = 2 << 20 // 2 MiB

type Service struct {
	root    *os.Root
	DataDir string
}

func New(dataDir string) (*Service, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(dataDir)
	if err != nil {
		return nil, err
	}
	return &Service{root: r, DataDir: dataDir}, nil
}

func (s *Service) Root() *os.Root { return s.root }

// CleanPath normalizes an API path to a slash-separated path relative
// to the data root. "" means the root itself. path.Clean("/"+p) can
// never contain "..", so traversal is impossible even before os.Root's
// own enforcement.
func CleanPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = path.Clean("/" + p)
	return strings.TrimPrefix(p, "/")
}

// fsName converts a cleaned path to what os.Root expects ("." for root).
func fsName(p string) string {
	if p == "" {
		return "."
	}
	return p
}

func validName(name string) error {
	if name == "" || name == "." || name == ".." ||
		strings.ContainsAny(name, `/\`) {
		return errors.New("非法文件名")
	}
	return nil
}

type Entry struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Dir     bool   `json:"dir"`
	ModTime int64  `json:"mtime"`
}

func (s *Service) List(p string) ([]Entry, error) {
	f, err := s.root.Open(fsName(p))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, errors.New("不是文件夹")
	}
	des, err := f.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(des))
	for _, de := range des {
		// 隐藏点开头的条目(回收站 .trash 等内部目录)
		if strings.HasPrefix(de.Name(), ".") {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		entries = append(entries, Entry{
			Name:    de.Name(),
			Size:    info.Size(),
			Dir:     de.IsDir(),
			ModTime: info.ModTime().UnixMilli(),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, nil
}

// ---- HTTP handlers ----

func (s *Service) HandleList(w http.ResponseWriter, r *http.Request) {
	p := CleanPath(r.URL.Query().Get("path"))
	entries, err := s.List(p)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"path": p, "entries": entries})
}

func (s *Service) HandleMkdir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	p := CleanPath(req.Path)
	if p == "" {
		httpx.Err(w, http.StatusBadRequest, "路径不能为空")
		return
	}
	if err := s.root.MkdirAll(p, 0o755); err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) HandleUpload(w http.ResponseWriter, r *http.Request) {
	dir := CleanPath(r.URL.Query().Get("path"))
	if dir != "" {
		if err := s.root.MkdirAll(dir, 0o755); err != nil {
			httpx.Err(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	mr, err := r.MultipartReader()
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "需要 multipart 上传")
		return
	}
	var saved []string
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			httpx.Err(w, http.StatusBadRequest, err.Error())
			return
		}
		name := path.Base(strings.ReplaceAll(part.FileName(), `\`, "/"))
		if name == "" || name == "." || name == "/" {
			part.Close()
			continue
		}
		if err := s.savePart(path.Join(dir, name), part); err != nil {
			part.Close()
			httpx.Err(w, http.StatusInternalServerError, err.Error())
			return
		}
		part.Close()
		saved = append(saved, name)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "saved": saved})
}

func (s *Service) savePart(p string, part *multipart.Part) error {
	f, err := s.root.Create(p)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, part); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (s *Service) HandleDownload(w http.ResponseWriter, r *http.Request) {
	p := CleanPath(r.URL.Query().Get("path"))
	if p == "" {
		httpx.Err(w, http.StatusBadRequest, "路径不能为空")
		return
	}
	f, err := s.root.Open(p)
	if err != nil {
		httpx.Err(w, http.StatusNotFound, "文件不存在")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		httpx.Err(w, http.StatusBadRequest, "不是文件")
		return
	}
	if r.URL.Query().Get("dl") == "1" {
		w.Header().Set("Content-Disposition",
			"attachment; filename*=UTF-8''"+url.PathEscape(fi.Name()))
	}
	http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
}

func (s *Service) HandleContent(w http.ResponseWriter, r *http.Request) {
	p := CleanPath(r.URL.Query().Get("path"))
	f, err := s.root.Open(fsName(p))
	if err != nil {
		httpx.Err(w, http.StatusNotFound, "文件不存在")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		httpx.Err(w, http.StatusBadRequest, "不是文件")
		return
	}
	if fi.Size() > maxTextPreview {
		httpx.Err(w, http.StatusRequestEntityTooLarge, "文件过大,请下载后查看")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.Copy(w, f)
}

// HandleWrite saves text content (markdown notes etc.), capped at 5 MiB.
func (s *Service) HandleWrite(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 6<<20))
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, "读取请求失败")
		return
	}
	if len(body) > 5<<20+1024 {
		httpx.Err(w, http.StatusRequestEntityTooLarge, "内容过大(上限 5MB)")
		return
	}
	if err := json.Unmarshal(body, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	p := CleanPath(req.Path)
	if p == "" {
		httpx.Err(w, http.StatusBadRequest, "路径不能为空")
		return
	}
	if dir := path.Dir(p); dir != "." {
		if err := s.root.MkdirAll(dir, 0o755); err != nil {
			httpx.Err(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	f, err := s.root.Create(p)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := f.WriteString(req.Content); err != nil {
		f.Close()
		httpx.Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := f.Close(); err != nil {
		httpx.Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) HandleRename(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path    string `json:"path"`
		NewName string `json:"newName"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	p := CleanPath(req.Path)
	if p == "" {
		httpx.Err(w, http.StatusBadRequest, "路径不能为空")
		return
	}
	if err := validName(req.NewName); err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.root.Rename(p, path.Join(path.Dir(p), req.NewName)); err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) HandleMove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		Dest string `json:"dest"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	src := CleanPath(req.Path)
	dest := CleanPath(req.Dest)
	if src == "" {
		httpx.Err(w, http.StatusBadRequest, "路径不能为空")
		return
	}
	if dest == src || strings.HasPrefix(dest+"/", src+"/") {
		httpx.Err(w, http.StatusBadRequest, "不能移动到自身内部")
		return
	}
	fi, err := s.root.Stat(fsName(dest))
	if err != nil || !fi.IsDir() {
		httpx.Err(w, http.StatusBadRequest, "目标文件夹不存在")
		return
	}
	if err := s.root.Rename(src, path.Join(dest, path.Base(src))); err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Service) HandleDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Paths []string `json:"paths"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	for _, raw := range req.Paths {
		p := CleanPath(raw)
		if p == "" {
			httpx.Err(w, http.StatusBadRequest, "不能删除根目录")
			return
		}
		if err := s.root.RemoveAll(p); err != nil {
			httpx.Err(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}
