package cloud

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"pocketdrive/internal/db"
)

type WebDAVMount struct {
	name, username, password string
	base                     *url.URL
	client                   *http.Client
}

func validDAVPath(p string) bool {
	for _, part := range strings.Split(p, "/") {
		if part == ".." || part == "." || strings.ContainsAny(part, "\\\x00") {
			return false
		}
	}
	return true
}

func newWebDAVMount(p *db.StoragePolicy) (*WebDAVMount, error) {
	u, err := url.Parse(p.Endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid WebDAV endpoint")
	}
	if !validDAVPath(u.Path) || !validDAVPath(p.BasePath) {
		return nil, errors.New("invalid WebDAV base path")
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/"
	if p.BasePath != "" {
		u.Path += strings.Trim(p.BasePath, "/") + "/"
	}
	u.RawPath = ""
	m := &WebDAVMount{name: p.Name, username: p.Username, password: p.Password, base: u}
	m.client = &http.Client{Transport: &http.Transport{
		DialContext: (&net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		Proxy:       http.ProxyFromEnvironment, ResponseHeaderTimeout: 30 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second, IdleConnTimeout: 90 * time.Second,
	}, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != u.Scheme || req.URL.Host != u.Host || !strings.HasPrefix(req.URL.Path, u.Path) || !validDAVPath(req.URL.Path) {
			return errors.New("WebDAV redirect outside mount")
		}
		return nil
	}}
	return m, nil
}

func (m *WebDAVMount) MountName() string { return m.name }
func (m *WebDAVMount) resource(rel string) (*url.URL, error) {
	if !validDAVPath(rel) {
		return nil, os.ErrPermission
	}
	u := *m.base
	u.Path += strings.TrimLeft(rel, "/")
	return &u, nil
}

func (m *WebDAVMount) request(ctx context.Context, method, rel string, body io.Reader, size int64, headers map[string]string) (*http.Response, error) {
	u, err := m.resource(rel)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.ContentLength = size
	}
	req.SetBasicAuth(m.username, m.password)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		res.Body.Close()
		switch res.StatusCode {
		case 404:
			return nil, os.ErrNotExist
		case 401, 403:
			return nil, os.ErrPermission
		case 409, 412:
			return nil, fmt.Errorf("WebDAV %s: conflict (%d)", method, res.StatusCode)
		}
		return nil, fmt.Errorf("WebDAV %s: HTTP %d", method, res.StatusCode)
	}
	return res, nil
}

type davResponse struct {
	Href  string `xml:"href"`
	Props []struct {
		Status string `xml:"status"`
		Prop   struct {
			Size     int64  `xml:"getcontentlength"`
			Modified string `xml:"getlastmodified"`
			Type     struct {
				Collection *struct{} `xml:"collection"`
			} `xml:"resourcetype"`
		} `xml:"prop"`
	} `xml:"propstat"`
}

func (m *WebDAVMount) propfind(ctx context.Context, rel, depth string) ([]Entry, error) {
	const body = `<?xml version="1.0"?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/><d:getcontentlength/><d:getlastmodified/></d:prop></d:propfind>`
	res, err := m.request(ctx, "PROPFIND", rel, strings.NewReader(body), int64(len(body)), map[string]string{"Depth": depth, "Content-Type": "application/xml; charset=utf-8"})
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusMultiStatus {
		return nil, errors.New("WebDAV: expected multistatus")
	}
	var doc struct {
		Responses []davResponse `xml:"response"`
	}
	if err := xml.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(&doc); err != nil {
		return nil, err
	}
	target, _ := m.resource(rel)
	wanted := strings.TrimRight(target.Path, "/")
	if wanted == "" {
		wanted = "/"
	}
	out := make([]Entry, 0)
	seen := make(map[string]bool)
	for _, item := range doc.Responses {
		u, err := url.Parse(item.Href)
		if err != nil {
			continue
		}
		u = res.Request.URL.ResolveReference(u)
		p := strings.TrimRight(u.Path, "/")
		if p == "" {
			p = "/"
		}
		if u.Host != m.base.Host || u.Scheme != m.base.Scheme || !validDAVPath(p) {
			continue
		}
		if depth == "0" {
			if p != wanted {
				continue
			}
		} else {
			if p == wanted || path.Dir(p) != wanted {
				continue
			}
		}
		if seen[p] {
			continue
		}
		e := Entry{Name: path.Base(p)}
		found := false
		for _, ps := range item.Props {
			parts := strings.Fields(ps.Status)
			if len(parts) < 2 || parts[1] != "200" {
				continue
			}
			found = true
			e.Dir = e.Dir || ps.Prop.Type.Collection != nil
			if ps.Prop.Size > 0 {
				e.Size = ps.Prop.Size
			}
			if t, err := http.ParseTime(ps.Prop.Modified); err == nil {
				e.Mtime = t.UnixMilli()
			}
		}
		if found {
			seen[p] = true
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		if out[i].Dir {
			return out[i].Name < out[j].Name
		}
		return out[i].Mtime > out[j].Mtime
	})
	return out, nil
}

func (m *WebDAVMount) List(ctx context.Context, rel string) ([]Entry, error) {
	if rel != "" {
		rel = strings.TrimRight(rel, "/") + "/"
	}
	return m.propfind(ctx, rel, "1")
}
func (m *WebDAVMount) Stat(ctx context.Context, rel string) (Entry, error) {
	entries, err := m.propfind(ctx, rel, "0")
	if err != nil {
		return Entry{}, err
	}
	if len(entries) != 1 {
		return Entry{}, os.ErrNotExist
	}
	return entries[0], nil
}
func (m *WebDAVMount) Ping(ctx context.Context) error {
	e, err := m.Stat(ctx, "")
	if err == nil && !e.Dir {
		return errors.New("WebDAV root is not a directory")
	}
	return err
}
func (m *WebDAVMount) mutate(ctx context.Context, method, rel string, body io.Reader, size int64, headers map[string]string) error {
	if strings.Trim(rel, "/") == "" {
		return os.ErrPermission
	}
	res, err := m.request(ctx, method, rel, body, size, headers)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	// A recursive DELETE may report partial failure using 207.
	if res.StatusCode == http.StatusMultiStatus {
		return errors.New("WebDAV operation was not completed for all resources")
	}
	return nil
}
func (m *WebDAVMount) Put(ctx context.Context, rel string, r io.Reader, size int64) error {
	if !validDAVPath(rel) {
		return os.ErrPermission
	}
	if err := m.ensureDir(ctx, path.Dir(rel)); err != nil {
		return err
	}
	return m.mutate(ctx, "PUT", rel, r, size, nil)
}

func (m *WebDAVMount) ensureDir(ctx context.Context, rel string) error {
	if rel == "." || rel == "" || rel == "/" {
		return nil
	}
	e, err := m.Stat(ctx, rel)
	if err == nil {
		if !e.Dir {
			return errors.New("WebDAV parent is not a directory")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := m.ensureDir(ctx, path.Dir(rel)); err != nil {
		return err
	}
	if err := m.Mkdir(ctx, rel); err != nil {
		// Another concurrent upload may have created the same parent.
		if e, statErr := m.Stat(ctx, rel); statErr != nil || !e.Dir {
			return err
		}
	}
	return nil
}
func (m *WebDAVMount) Mkdir(ctx context.Context, rel string) error {
	return m.mutate(ctx, "MKCOL", rel, nil, 0, nil)
}
func (m *WebDAVMount) Delete(ctx context.Context, rel string) error {
	return m.mutate(ctx, "DELETE", rel, nil, 0, nil)
}
func (m *WebDAVMount) Rename(ctx context.Context, src, dst string) error {
	if strings.Trim(dst, "/") == "" {
		return os.ErrPermission
	}
	if src == dst {
		return nil
	}
	if strings.HasPrefix(strings.Trim(dst, "/")+"/", strings.Trim(src, "/")+"/") {
		return errors.New("cannot move directory into itself")
	}
	u, err := m.resource(dst)
	if err != nil {
		return err
	}
	return m.mutate(ctx, "MOVE", src, nil, 0, map[string]string{"Destination": u.String(), "Overwrite": "F"})
}
func (m *WebDAVMount) WalkFiles(ctx context.Context, rel string, fn func(string, int64, time.Time) error) error {
	var walk func(string, int) error
	walk = func(sub string, depth int) error {
		if depth > 128 {
			return errors.New("WebDAV directory nesting too deep")
		}
		dir := rel
		if sub != "" {
			dir = path.Join(rel, sub)
		}
		entries, err := m.List(ctx, dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			name := path.Join(sub, e.Name)
			if e.Dir {
				err = walk(name, depth+1)
			} else {
				err = fn(name, e.Size, time.UnixMilli(e.Mtime))
			}
			if err != nil {
				return err
			}
		}
		return nil
	}
	return walk("", 0)
}
func (m *WebDAVMount) PresignGet(context.Context, string, string, bool) (string, error) {
	return "", nil
}

// Reads reopen GET at the requested offset, keeping memory independent of size.
type davReader struct {
	m         *WebDAVMount
	ctx       context.Context
	rel       string
	size, off int64
	body      io.ReadCloser
	closed    bool
}

func (m *WebDAVMount) Open(ctx context.Context, rel string) (io.ReadSeekCloser, Entry, error) {
	e, err := m.Stat(ctx, rel)
	if err != nil {
		return nil, e, err
	}
	if e.Dir {
		return nil, e, errors.New("cannot open directory")
	}
	return &davReader{m: m, ctx: ctx, rel: rel, size: e.Size}, e, nil
}
func (f *davReader) Read(p []byte) (int, error) {
	if f.closed {
		return 0, os.ErrClosed
	}
	if len(p) == 0 {
		return 0, nil
	}
	if f.off >= f.size {
		return 0, io.EOF
	}
	if f.body == nil {
		headers := map[string]string{"Accept-Encoding": "identity"}
		if f.off > 0 {
			headers["Range"] = "bytes=" + strconv.FormatInt(f.off, 10) + "-"
		}
		res, err := f.m.request(f.ctx, "GET", f.rel, nil, 0, headers)
		if err != nil {
			return 0, err
		}
		if f.off > 0 {
			if res.StatusCode == http.StatusPartialContent {
				if !strings.HasPrefix(res.Header.Get("Content-Range"), fmt.Sprintf("bytes %d-", f.off)) {
					res.Body.Close()
					return 0, errors.New("invalid WebDAV range response")
				}
			} else if res.StatusCode == http.StatusOK {
				if _, err := io.CopyN(io.Discard, res.Body, f.off); err != nil {
					res.Body.Close()
					return 0, err
				}
			} else {
				res.Body.Close()
				return 0, errors.New("invalid WebDAV download response")
			}
		}
		f.body = res.Body
	}
	if int64(len(p)) > f.size-f.off {
		p = p[:f.size-f.off]
	}
	n, err := f.body.Read(p)
	f.off += int64(n)
	if err == io.EOF && f.off < f.size {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}
func (f *davReader) Seek(offset int64, whence int) (int64, error) {
	if f.closed {
		return 0, os.ErrClosed
	}
	var base int64
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		base = f.off
	case io.SeekEnd:
		base = f.size
	default:
		return 0, errors.New("invalid seek")
	}
	next := base + offset
	if next < 0 || (offset > 0 && next < base) {
		return 0, errors.New("invalid seek offset")
	}
	if next != f.off && f.body != nil {
		f.body.Close()
		f.body = nil
	}
	f.off = next
	return next, nil
}
func (f *davReader) Close() error {
	f.closed = true
	if f.body != nil {
		return f.body.Close()
	}
	return nil
}
