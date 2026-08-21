package dav

// PROPFIND 在外部存储上的请求放大回归测试。
//
// 症状:手机 WebDAV 客户端打开 R2 上一千多首歌的文件夹,一直转圈打不开。
// 根因是 x/net/webdav 的 walkFS 把 Readdir 的结果整份丢掉,再对每个孩子
// 重新调一次 FileSystem.Stat——落到 S3 上就是一个文件一次 HEAD,一千次
// 串行往返,客户端超时。另外不带 Depth 的 PROPFIND 按 RFC 是 infinity,
// 对着挂载点等于把整个桶递归列一遍。
//
// 这里起一个够用的假 S3,数一次 PROPFIND 到底往存储上打了多少个请求:
// 断言的是"请求数不随文件数增长",不是具体某个数字。

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pocketdrive/internal/cloud"
	"pocketdrive/internal/db"
)

const fakeMount = "FAKE"

const fakeETag = `"0123456789abcdef0123456789abcdef"`

// fakeS3 是够 minio-go 跑通 ListObjectsV2 / HEAD / GET / 分片上传的最小
// S3 实现:读的三个动作分别计数,写的请求逐条记下来(见 writes)。
type fakeS3 struct {
	keys  []string // 桶里全部对象 key
	heads atomic.Int64
	lists atomic.Int64
	gets  atomic.Int64

	mu      sync.Mutex
	written []string
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 路径式寻址(endpoint 是 IP:端口):/{bucket}/{key}
	_, key, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/"), "/")
	switch {
	case r.Method == http.MethodHead:
		f.heads.Add(1)
		f.serveObject(w, r, key)
	case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
		f.lists.Add(1)
		f.writeList(w, r)
	case r.Method == http.MethodGet:
		f.gets.Add(1)
		f.serveObject(w, r, key)
	default:
		f.serveWrite(w, r, key)
	}
}

// ---- 写请求 ----

// serveWrite 兜住所有会改动桶内对象的请求,并逐条记下来:"桶上到底被写
// 过没有"是 proppatch_test.go 那组回归测试的判据。
//
// s3WriteFile 上传的是未知长度的流(size=-1),minio 对这种一律走分片,
// 不发单个 PUT:CreateMultipartUpload → UploadPart → CompleteMultipart-
// Upload。三步都得应答上,写入路径才能在测试里真的跑通。
func (f *fakeS3) serveWrite(w http.ResponseWriter, r *http.Request, key string) {
	n, _ := io.Copy(io.Discard, r.Body)
	// 分片走的是 AWS 流式签名,body 外面还裹着一层分块框架;真正写进对象
	// 的字节数在这个头里。记这个数,"传上去的到底是不是空的"才看得出来。
	if v, err := strconv.ParseInt(r.Header.Get("x-amz-decoded-content-length"), 10, 64); err == nil {
		n = v
	}
	q := r.URL.Query()
	f.record(r.Method, key, q, n)
	switch {
	case r.Method == http.MethodPost && q.Has("uploads"):
		writeXML(w, xmlInitiateResult{Bucket: "testbucket", Key: key, UploadID: "fake-upload-id"})
	case r.Method == http.MethodPost && q.Get("uploadId") != "":
		writeXML(w, xmlCompleteResult{Bucket: "testbucket", Key: key, ETag: fakeETag})
	default: // UploadPart、单个 PUT、DELETE
		w.Header().Set("ETag", fakeETag)
		w.WriteHeader(http.StatusOK)
	}
}

// record 记一条写请求。分片上传的三步长得很像,把 uploadId 之外的 query
// 也带上,断言失败时能一眼看出是哪一步、body 有多少字节。
func (f *fakeS3) record(method, key string, q url.Values, body int64) {
	desc := method + " " + key
	rest := url.Values{}
	for k, v := range q {
		if k != "uploadId" { // 每次跑都不一样,记了没用
			rest[k] = v
		}
	}
	if s := rest.Encode(); s != "" {
		desc += "?" + s
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.written = append(f.written, fmt.Sprintf("%s (body %d 字节)", desc, body))
}

// writes 返回目前为止所有改动过桶内对象的请求。
func (f *fakeS3) writes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.written)
}

// objBody 是每个对象的内容(所有 key 一样,够切 Range 就行)。
var objBody = []byte("pocketdrive webdav lazy read payload")

var objSize = int64(len(objBody))

// serveObject 应答 HEAD/GET;ServeContent 顺带把 Range 和 206 处理好。
func (f *fakeS3) serveObject(w http.ResponseWriter, r *http.Request, key string) {
	if !slices.Contains(f.keys, key) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("ETag", fakeETag)
	http.ServeContent(w, r, key, time.Unix(1700000000, 0).UTC(), bytes.NewReader(objBody))
}

// ---- XML 应答 ----

type xmlObject struct {
	XMLName      xml.Name `xml:"Contents"`
	Key          string
	LastModified string
	ETag         string
	Size         int64
	StorageClass string
}

type xmlPrefix struct {
	XMLName xml.Name `xml:"CommonPrefixes"`
	Prefix  string
}

type xmlListResult struct {
	XMLName               xml.Name `xml:"ListBucketResult"`
	Name                  string
	Prefix                string
	Delimiter             string
	MaxKeys               int
	KeyCount              int
	IsTruncated           bool
	NextContinuationToken string `xml:",omitempty"`
	Contents              []xmlObject
	CommonPrefixes        []xmlPrefix
}

type xmlInitiateResult struct {
	XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
	Bucket   string
	Key      string
	UploadID string `xml:"UploadId"`
}

type xmlCompleteResult struct {
	XMLName xml.Name `xml:"CompleteMultipartUploadResult"`
	Bucket  string
	Key     string
	ETag    string
}

func writeXML(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/xml")
	_, _ = w.Write([]byte(xml.Header))
	_ = xml.NewEncoder(w).Encode(v)
}

func (f *fakeS3) writeList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	prefix, delim := q.Get("prefix"), q.Get("delimiter")
	maxKeys := 1000
	if v, err := strconv.Atoi(q.Get("max-keys")); err == nil && v > 0 {
		maxKeys = v
	}
	start, _ := strconv.Atoi(q.Get("continuation-token"))

	// 先按 delimiter 折叠成一份有序条目,再按 max-keys 切页
	type item struct {
		key   string
		isDir bool
	}
	var items []item
	seen := map[string]bool{}
	for _, k := range f.keys {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := strings.TrimPrefix(k, prefix)
		if i := strings.Index(rest, delim); delim != "" && i >= 0 {
			cp := prefix + rest[:i+len(delim)]
			if !seen[cp] {
				seen[cp] = true
				items = append(items, item{cp, true})
			}
			continue
		}
		items = append(items, item{k, false})
	}

	res := xmlListResult{
		Name: r.URL.Path, Prefix: prefix, Delimiter: delim, MaxKeys: maxKeys,
	}
	end := min(start+maxKeys, len(items))
	if start > len(items) {
		end = len(items)
	}
	for _, it := range items[min(start, len(items)):end] {
		if it.isDir {
			res.CommonPrefixes = append(res.CommonPrefixes, xmlPrefix{Prefix: it.key})
			continue
		}
		res.Contents = append(res.Contents, xmlObject{
			Key:          it.key,
			LastModified: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
			ETag:         fakeETag,
			Size:         objSize, StorageClass: "STANDARD",
		})
	}
	res.KeyCount = len(res.Contents) + len(res.CommonPrefixes)
	if end < len(items) {
		res.IsTruncated = true
		res.NextContinuationToken = strconv.Itoa(end)
	}
	writeXML(w, res)
}

// ---- 测试装配 ----

// newFakeDav 起假 S3 + 一个挂到它上面的真 WebDAV handler。
func newFakeDav(t *testing.T, keys []string) (http.Handler, *fakeS3, *cloud.Service) {
	t.Helper()
	fake := &fakeS3{keys: keys}
	s3srv := httptest.NewServer(fake)
	t.Cleanup(s3srv.Close)

	dir := t.TempDir()
	gdb, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := gdb.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	// Region 写死,免得 minio 再去问一次 GetBucketLocation
	if err := gdb.Create(&db.StoragePolicy{
		Name: fakeMount, Type: "s3", Endpoint: s3srv.URL, Region: "us-east-1",
		Bucket: "testbucket", AccessKey: "key", SecretKey: "secret",
	}).Error; err != nil {
		t.Fatalf("写入策略: %v", err)
	}

	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	root, err := os.OpenRoot(dataDir)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	t.Cleanup(func() { _ = root.Close() })

	svc := cloud.New(gdb)
	if _, _, ok := svc.Resolve("@" + fakeMount); !ok {
		t.Fatal("挂载没加载出来")
	}
	return Handler(root, svc), fake, svc
}

func propfind(t *testing.T, h http.Handler, p string, depth string) string {
	t.Helper()
	req := httptest.NewRequest("PROPFIND", p, nil)
	if depth != "" {
		req.Header.Set("Depth", depth)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND %s (Depth:%q) → %d, want 207: %s", p, depth, w.Code, w.Body.String())
	}
	return w.Body.String()
}

// ---- 用例 ----

// 主症状:一千首歌的文件夹。修好之前每个文件一次 HEAD(1000+ 次串行
// 往返 → 客户端超时);修好之后请求数与文件数无关。
func TestPropfindLargeFolderDoesNotFanOut(t *testing.T) {
	const n = 800
	keys := make([]string, n)
	for i := range keys {
		keys[i] = fmt.Sprintf("music/song%04d.mp3", i)
	}
	h, fake, _ := newFakeDav(t, keys)

	body := propfind(t, h, "/dav/@"+fakeMount+"/music", "1")

	// 目录本身 + n 个文件,一个都不能少
	if got := strings.Count(body, "<D:href>"); got != n+1 {
		t.Errorf("列出 %d 项, want %d", got, n+1)
	}
	for _, want := range []string{"song0000.mp3", "song0799.mp3"} {
		if !strings.Contains(body, want) {
			t.Errorf("列表里没有 %s", want)
		}
	}
	if !strings.Contains(body, fmt.Sprintf("<D:getcontentlength>%d", objSize)) {
		t.Error("没给出文件大小,播放器会读不出时长")
	}

	t.Logf("HEAD=%d LIST=%d GET=%d(文件数 %d)",
		fake.heads.Load(), fake.lists.Load(), fake.gets.Load(), n)

	// 下面三条才是重点:修好之前 HEAD 是 1601 次
	if got := fake.heads.Load(); got > 2 {
		t.Errorf("HEAD %d 次(文件数 %d),请求数在随文件数增长——逐个回源 Stat 又回来了", got, n)
	}
	if got := fake.lists.Load(); got > 4 {
		t.Errorf("LIST %d 次,期望常数次", got)
	}
	// entryInfo.ContentType 一旦没了,PROPFIND 会为每个文件回源嗅探 512 字节
	if got := fake.gets.Load(); got != 0 {
		t.Errorf("回源 GET %d 次,期望 0(类型该按扩展名给,内容该等到真读时再取)", got)
	}
}

// 不带 Depth 的 PROPFIND:RFC 说按 infinity,真照做就是把整个桶递归列
// 一遍。按客户端实际想要的 Depth: 1 应答。
func TestPropfindWithoutDepthDoesNotRecurse(t *testing.T) {
	h, fake, _ := newFakeDav(t, []string{
		"music/a.mp3", "music/sub/b.mp3", "music/sub/deep/c.mp3",
	})

	body := propfind(t, h, "/dav/@"+fakeMount, "")

	if !strings.Contains(body, "music") {
		t.Fatalf("没列出一级子目录: %s", body)
	}
	for _, deep := range []string{"a.mp3", "b.mp3", "c.mp3"} {
		if strings.Contains(body, deep) {
			t.Errorf("递归到了 %s,不带 Depth 的请求应当只列一层", deep)
		}
	}
	if l := fake.lists.Load(); l > 2 {
		t.Errorf("LIST %d 次,只列一层不该翻这么多", l)
	}
}

// 显式写了 Depth: infinity 的客户端照旧递归——只兜底缺省值,不改语义。
func TestPropfindExplicitInfinityStillRecurses(t *testing.T) {
	h, _, _ := newFakeDav(t, []string{
		"music/a.mp3", "music/sub/b.mp3", "music/sub/deep/c.mp3",
	})

	body := propfind(t, h, "/dav/@"+fakeMount, "infinity")

	for _, want := range []string{"a.mp3", "b.mp3", "c.mp3"} {
		if !strings.Contains(body, want) {
			t.Errorf("Depth: infinity 没有列出 %s: %s", want, body)
		}
	}
}

// OpenFile 不再预开对象之后,读取本身必须照旧正确:HEAD 只给元信息、
// GET 拿到全文、Range 拿到对应那一段(播放器拖进度条靠它)。
func TestCloudFileReadStaysCorrect(t *testing.T) {
	const p = "/dav/@" + fakeMount + "/music/a.mp3"
	h, fake, svc := newFakeDav(t, []string{"music/a.mp3"})
	setDavDirect(t, svc, false) // 关直连,强制走中转,才会用到 s3ReadFile

	t.Run("HEAD 不回源取字节", func(t *testing.T) {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodHead, p, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("HEAD → %d, want 200", w.Code)
		}
		if got := w.Header().Get("Content-Length"); got != strconv.FormatInt(objSize, 10) {
			t.Errorf("Content-Length = %q, want %d", got, objSize)
		}
		if got := fake.gets.Load(); got != 0 {
			t.Errorf("HEAD 触发了 %d 次回源 GET,期望 0", got)
		}
	})

	t.Run("GET 拿到全文", func(t *testing.T) {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, p, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GET → %d, want 200", w.Code)
		}
		if got := w.Body.Bytes(); !bytes.Equal(got, objBody) {
			t.Errorf("内容 = %q, want %q", got, objBody)
		}
	})

	t.Run("Range 拿到对应片段", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		req.Header.Set("Range", "bytes=12-20")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusPartialContent {
			t.Fatalf("Range GET → %d, want 206", w.Code)
		}
		if got, want := w.Body.Bytes(), objBody[12:21]; !bytes.Equal(got, want) {
			t.Errorf("片段 = %q, want %q", got, want)
		}
	})
}

// S3Mount.Stat 判断"是不是目录"时会提前从 ListObjects 的 channel 里跳出
// 来。minio 的生产者 goroutine 只有等 channel 被读干净才退出,漏一次就是
// 漏一个 goroutine 加一条在途连接——WebDAV 每列一个目录就漏一次,挂着
// 同步类客户端的话一天能攒出几千个。
func TestDirStatDoesNotLeakGoroutines(t *testing.T) {
	// 目录下要有好几个对象:Stat 只探第一条(MaxKeys:1)就跳出,后面还有
	// 分页没读完,生产者才会真的卡在发送上。只放一个对象是探不出问题的。
	_, _, svc := newFakeDav(t, []string{
		"music/a.mp3", "music/b.mp3", "music/c.mp3", "music/d.mp3", "music/e.mp3",
	})
	m, _, ok := svc.Resolve("@" + fakeMount)
	if !ok {
		t.Fatal("挂载没加载出来")
	}
	ctx := context.Background()

	// 预热:先把连接池和 SDK 的常驻 goroutine 建起来,不然会算进增量
	for range 3 {
		if _, err := m.Stat(ctx, "music"); err != nil {
			t.Fatalf("Stat: %v", err)
		}
	}
	before := runtime.NumGoroutine()

	const n = 50
	for range n {
		if _, err := m.Stat(ctx, "music"); err != nil {
			t.Fatalf("Stat: %v", err)
		}
	}

	// goroutine 退出是异步的,给一小段时间收敛;漏的话是 +n,不是 +几
	var after int
	for range 40 {
		if after = runtime.NumGoroutine(); after-before <= n/5 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("%d 次目录 Stat 之后 goroutine 从 %d 涨到 %d,ListObjects 的生产者没退出",
		n, before, after)
}
