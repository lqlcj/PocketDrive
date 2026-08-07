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

	"gorm.io/gorm"

	"pocketdrive/internal/cloud"
	"pocketdrive/internal/httpx"
)

const maxTextPreview = 2 << 20 // 2 MiB

type Service struct {
	root    *os.Root
	DataDir string
	tmpDir  string // 分片上传暂存目录(DB 同级,不在网盘里)
	cloud   *cloud.Service
	db      *gorm.DB // 分片上传会话(断点续传需要跨请求/跨重启存活)
	space   LocalSpace
}

// LocalSpace 是本机容量检查的钩子,由 internal/storage 实现。
//
// 之所以用接口注入而不是直接 import:storage 要拿 files 的 fs.FS 才能
// 构造,反过来再依赖回来就成了构造顺序上的死结。
type LocalSpace interface {
	// CheckLocal 在写入前判断还装不装得下;size 未知时传 0
	CheckLocal(size int64) error
	// AddUsage 写完就地累加,免得等下一轮全量统计
	AddUsage(delta int64)
	// UploadLimit returns the maximum request body size allowed for a local upload.
	// Zero means no configured quota.
	UploadLimit() int64
}

// SetLocalSpace 在 storage 构造好之后回填。没设时所有检查都放行。
func (s *Service) SetLocalSpace(sp LocalSpace) { s.space = sp }

func (s *Service) checkLocal(size int64) error {
	if s.space == nil {
		return nil
	}
	return s.space.CheckLocal(size)
}

func (s *Service) AddUsage(delta int64) {
	if s.space != nil {
		s.space.AddUsage(delta)
	}
}

func (s *Service) localFileSize(p string) int64 {
	fi, err := s.root.Stat(p)
	if err != nil || fi.IsDir() {
		return 0
	}
	return fi.Size()
}

func New(dataDir, tmpDir string, cloudSvc *cloud.Service, gdb *gorm.DB) (*Service, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(dataDir)
	if err != nil {
		return nil, err
	}
	return &Service{root: r, DataDir: dataDir, tmpDir: tmpDir, cloud: cloudSvc, db: gdb}, nil
}

func (s *Service) Root() *os.Root { return s.root }

// resolveMount 处理外部存储路径:返回挂载与挂载内相对路径。挂载名
// 不存在时直接写 404 并返回 handled=false 之外的信号——调用方约定:
// (nil, "", true) 表示"是挂载路径但已出错响应,不要继续"。
func (s *Service) resolveMount(w http.ResponseWriter, p string) (*cloud.S3Mount, string, bool) {
	m, rel, ok := s.cloud.Resolve(p)
	if !ok {
		httpx.Err(w, http.StatusNotFound, "外部存储不存在或未挂载")
		return nil, "", true
	}
	return m, rel, false
}

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
		if entries[i].Dir {
			return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
		}
		// 文件按修改时间倒序:新传的排最前
		return entries[i].ModTime > entries[j].ModTime
	})
	return entries, nil
}

// ---- HTTP handlers ----

func (s *Service) HandleList(w http.ResponseWriter, r *http.Request) {
	p := CleanPath(r.URL.Query().Get("path"))
	if cloud.IsMountPath(p) {
		m, rel, bad := s.resolveMount(w, p)
		if bad {
			return
		}
		ces, err := m.List(r.Context(), rel)
		if err != nil {
			httpx.Err(w, http.StatusBadGateway, "外部存储读取失败: "+err.Error())
			return
		}
		entries := make([]Entry, len(ces))
		for i, e := range ces {
			entries[i] = Entry{Name: e.Name, Size: e.Size, Dir: e.Dir, ModTime: e.Mtime}
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"path": p, "entries": entries})
		return
	}
	entries, err := s.List(p)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	// 根目录追加外部存储挂载点(虚拟文件夹,置于文件夹区最前)
	if p == "" {
		names := s.cloud.Names()
		sort.Strings(names)
		mounts := make([]Entry, len(names))
		for i, n := range names {
			mounts[i] = Entry{Name: cloud.MountPrefix + n, Dir: true}
		}
		entries = append(mounts, entries...)
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
	if cloud.IsMountPath(p) {
		m, rel, bad := s.resolveMount(w, p)
		if bad {
			return
		}
		if rel == "" {
			httpx.Err(w, http.StatusBadRequest, "挂载点已存在")
			return
		}
		if err := m.Mkdir(r.Context(), rel); err != nil {
			httpx.Err(w, http.StatusBadGateway, err.Error())
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
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
	var mnt *cloud.S3Mount
	var mntRel string
	if cloud.IsMountPath(dir) {
		m, rel, bad := s.resolveMount(w, dir)
		if bad {
			return
		}
		mnt, mntRel = m, rel
	} else if dir != "" {
		if err := s.root.MkdirAll(dir, 0o755); err != nil {
			httpx.Err(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	// 整个请求的大小是已知的,能在动手之前就把装不下的挡回去
	if mnt == nil && r.ContentLength > 0 {
		if err := s.checkLocal(r.ContentLength); err != nil {
			httpx.Err(w, http.StatusInsufficientStorage, err.Error())
			return
		}
	}
	// 配额是软限制:知道还剩多少时,用 MaxBytesReader 把请求体限制在剩余
	// 额度内。必须先包好 Body 再 MultipartReader——Go 1.21+ 里对同一个
	// Request 第二次调用 MultipartReader 会报 "called twice"。
	if mnt == nil && s.space != nil {
		if limit := s.space.UploadLimit(); limit > 0 {
			r.Body = http.MaxBytesReader(w, r.Body, limit+1<<20)
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
		if mnt != nil {
			// 配额是软限制:统计还没跑过时放行,不会因为算不出用量就堵死上传
			if err := s.cloud.CheckQuota(mnt.Name, 0); err != nil {
				part.Close()
				httpx.Err(w, http.StatusInsufficientStorage, err.Error())
				return
			}
			// 中转直推 S3:size 未知走流式 multipart(8MB 内存缓冲)
			err = mnt.Put(r.Context(), path.Join(mntRel, name), part, -1)
			if err != nil {
				part.Close()
				httpx.Err(w, http.StatusBadGateway, "上传到外部存储失败: "+err.Error())
				return
			}
		} else {
			if err := s.checkLocal(0); err != nil {
				part.Close()
				httpx.Err(w, http.StatusInsufficientStorage, err.Error())
				return
			}
			delta, err := s.savePart(path.Join(dir, name), part)
			if err != nil {
				part.Close()
				httpx.Err(w, http.StatusInternalServerError, "保存上传文件失败")
				return
			}
			s.AddUsage(delta)
		}
		part.Close()
		saved = append(saved, name)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "saved": saved})
}

func (s *Service) savePart(p string, part *multipart.Part) (int64, error) {
	oldSize := s.localFileSize(p)
	f, err := s.root.Create(p)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(f, part)
	if err != nil {
		f.Close()
		return 0, err
	}
	return n - oldSize, f.Close()
}

func (s *Service) HandleDownload(w http.ResponseWriter, r *http.Request) {
	p := CleanPath(r.URL.Query().Get("path"))
	if p == "" {
		httpx.Err(w, http.StatusBadRequest, "路径不能为空")
		return
	}
	if cloud.IsMountPath(p) {
		m, rel, bad := s.resolveMount(w, p)
		if bad {
			return
		}
		// 302 到预签名 URL:浏览器/播放器直连存储桶,不过 VPS 中转,
		// 媒体加载不受同源策略限制,无需给桶配任何 CORS
		u, err := m.PresignGet(r.Context(), rel, path.Base(rel),
			r.URL.Query().Get("dl") == "1")
		if err != nil {
			httpx.Err(w, http.StatusBadGateway, "生成下载链接失败: "+err.Error())
			return
		}
		http.Redirect(w, r, u, http.StatusFound)
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
	if cloud.IsMountPath(p) {
		m, rel, bad := s.resolveMount(w, p)
		if bad {
			return
		}
		obj, info, err := m.Open(r.Context(), rel)
		if err != nil {
			httpx.Err(w, http.StatusNotFound, "文件不存在")
			return
		}
		defer obj.Close()
		if info.Size > maxTextPreview {
			httpx.Err(w, http.StatusRequestEntityTooLarge, "文件过大,请下载后查看")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.Copy(w, obj)
		return
	}
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
	if cloud.IsMountPath(p) {
		m, rel, bad := s.resolveMount(w, p)
		if bad {
			return
		}
		if rel == "" {
			httpx.Err(w, http.StatusBadRequest, "不能往挂载点根写入空路径")
			return
		}
		if err := m.Put(r.Context(), rel, strings.NewReader(req.Content),
			int64(len(req.Content))); err != nil {
			httpx.Err(w, http.StatusBadGateway, "写入外部存储失败: "+err.Error())
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	oldSize := s.localFileSize(p)
	delta := int64(len(req.Content)) - oldSize
	if delta > 0 {
		if err := s.checkLocal(delta); err != nil {
			httpx.Err(w, http.StatusInsufficientStorage, err.Error())
			return
		}
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
	s.AddUsage(delta)
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
	if cloud.IsMountPath(p) {
		m, rel, bad := s.resolveMount(w, p)
		if bad {
			return
		}
		if rel == "" {
			httpx.Err(w, http.StatusBadRequest, "挂载点名称请到「存储策略」里修改")
			return
		}
		if err := m.Rename(r.Context(), rel,
			path.Join(path.Dir(rel), req.NewName)); err != nil {
			httpx.Err(w, http.StatusBadGateway, err.Error())
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
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
	// 外部存储:只支持同一挂载内移动(跨存储请下载后重新上传)
	if cloud.IsMountPath(src) || cloud.IsMountPath(dest) {
		ms, srel, okS := s.cloud.Resolve(src)
		md, drel, okD := s.cloud.Resolve(dest)
		if !okS || !okD || ms != md {
			httpx.Err(w, http.StatusBadRequest, "暂不支持跨存储移动,请下载后再上传到目标存储")
			return
		}
		if srel == "" {
			httpx.Err(w, http.StatusBadRequest, "挂载点本身不能移动")
			return
		}
		if drel != "" {
			if fi, err := md.Stat(r.Context(), drel); err != nil || !fi.Dir {
				httpx.Err(w, http.StatusBadRequest, "目标文件夹不存在")
				return
			}
		}
		if err := ms.Rename(r.Context(), srel, path.Join(drel, path.Base(srel))); err != nil {
			httpx.Err(w, http.StatusBadGateway, err.Error())
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
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
