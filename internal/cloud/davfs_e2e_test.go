package cloud

// WebDAV 端到端:起真实 webdav.Handler + DavFS,用真实 HTTP 动词
// (PROPFIND/MKCOL/PUT/GET/MOVE/DELETE)跑一遍,确认手机播放器/文件
// 管理器看到的结构与网页端一致。凭据同 s3_e2e_test.go。

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/webdav"
)

type davClient struct {
	t   *testing.T
	url string
}

func (c *davClient) do(method, p string, body io.Reader, hdr map[string]string) *http.Response {
	c.t.Helper()
	req, err := http.NewRequest(method, c.url+p, body)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, p, err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, p, err)
	}
	return resp
}

// text 发请求并读全响应体,顺带断言状态码在 2xx。
func (c *davClient) text(method, p string, body io.Reader, hdr map[string]string) string {
	c.t.Helper()
	resp := c.do(method, p, body, hdr)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		c.t.Fatalf("%s %s → %d: %s", method, p, resp.StatusCode, b)
	}
	return string(b)
}

func (c *davClient) propfind(p string) string {
	return c.text("PROPFIND", p, nil, map[string]string{"Depth": "1"})
}

func TestE2EWebDAV(t *testing.T) {
	m := testMount(t)

	// 只挂一个存储,不碰 DB——DavFS 只用到 Resolve/Names
	svc := &Service{mounts: map[string]*S3Mount{m.Name: m}}

	localDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(localDir, "本机文件.txt"), []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(&webdav.Handler{
		Prefix:     "/dav",
		FileSystem: NewDavFS(svc, localDir),
		LockSystem: webdav.NewMemLS(),
	})
	defer srv.Close()
	c := &davClient{t: t, url: srv.URL + "/dav"}

	root := fmt.Sprintf("e2e-dav-%d", time.Now().UnixNano())
	mnt := "/@" + m.Name
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		// 用例正常跑完时 root 已被 DELETE 删掉,这里只兜住中途失败的残留
		if err := m.Delete(ctx, root); err != nil && !strings.Contains(err.Error(), "不存在") {
			t.Logf("清理 %s 失败(需手动删): %v", root, err)
		}
	})

	t.Run("根目录列出本机文件和挂载点", func(t *testing.T) {
		body := c.propfind("/")
		if !strings.Contains(body, url.PathEscape("本机文件.txt")) {
			t.Error("根目录没有列出本机文件")
		}
		if !strings.Contains(body, "@"+m.Name) {
			t.Errorf("根目录没有列出挂载点 @%s", m.Name)
		}
	})

	t.Run("MKCOL 建目录", func(t *testing.T) {
		resp := c.do("MKCOL", mnt+"/"+root, nil, nil)
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("MKCOL → %d, want 201", resp.StatusCode)
		}
	})

	const body = "hello from webdav\n"
	file := mnt + "/" + root + "/" + url.PathEscape("歌曲 song.txt")

	t.Run("PUT 上传", func(t *testing.T) {
		resp := c.do("PUT", file, strings.NewReader(body), nil)
		resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			t.Fatalf("PUT → %d", resp.StatusCode)
		}
	})

	t.Run("PROPFIND 目录看到文件", func(t *testing.T) {
		got := c.propfind(mnt + "/" + root)
		if !strings.Contains(got, url.PathEscape("歌曲 song.txt")) {
			t.Errorf("目录列表里没有刚上传的文件: %s", got)
		}
		if !strings.Contains(got, fmt.Sprintf("<D:getcontentlength>%d", len(body))) {
			t.Errorf("文件大小不对,播放器会读不出时长: %s", got)
		}
	})

	t.Run("GET 下载", func(t *testing.T) {
		if got := c.text("GET", file, nil, nil); got != body {
			t.Fatalf("GET = %q, want %q", got, body)
		}
	})

	t.Run("Range 请求", func(t *testing.T) {
		// 播放器拖进度条靠这个;必须是 206 且只回该段
		resp := c.do("GET", file, nil, map[string]string{"Range": "bytes=6-10"})
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusPartialContent {
			t.Fatalf("Range GET → %d, want 206", resp.StatusCode)
		}
		got, _ := io.ReadAll(resp.Body)
		if want := body[6:11]; string(got) != want {
			t.Errorf("Range = %q, want %q", got, want)
		}
	})

	t.Run("MOVE 改名", func(t *testing.T) {
		dst := c.url + mnt + "/" + root + "/" + url.PathEscape("改名 renamed.txt")
		resp := c.do("MOVE", file, nil, map[string]string{"Destination": dst})
		resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			t.Fatalf("MOVE → %d", resp.StatusCode)
		}
		if got := c.text("GET", mnt+"/"+root+"/"+url.PathEscape("改名 renamed.txt"), nil, nil); got != body {
			t.Errorf("改名后内容 = %q", got)
		}
	})

	t.Run("挂载点本身不可删", func(t *testing.T) {
		resp := c.do("DELETE", mnt, nil, nil)
		resp.Body.Close()
		if resp.StatusCode/100 == 2 {
			t.Error("删除挂载点应当被拒绝")
		}
	})

	t.Run("不存在的挂载返回 404", func(t *testing.T) {
		resp := c.do("PROPFIND", "/@根本没挂过", nil, map[string]string{"Depth": "1"})
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("→ %d, want 404", resp.StatusCode)
		}
	})

	t.Run("本机文件仍可读", func(t *testing.T) {
		if got := c.text("GET", "/"+url.PathEscape("本机文件.txt"), nil, nil); got != "local" {
			t.Errorf("本机文件 = %q, want local", got)
		}
	})

	t.Run("DELETE 目录", func(t *testing.T) {
		resp := c.do("DELETE", mnt+"/"+root, nil, nil)
		resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			t.Fatalf("DELETE → %d", resp.StatusCode)
		}
		got := c.do("GET", mnt+"/"+root+"/"+url.PathEscape("改名 renamed.txt"), nil, nil)
		got.Body.Close()
		if got.StatusCode != http.StatusNotFound {
			t.Errorf("删除后仍可读 → %d", got.StatusCode)
		}
	})
}
