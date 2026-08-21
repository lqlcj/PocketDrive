package dav

// PROPPATCH 把外部存储上的文件清成 0 字节的回归测试。
//
// 症状:挂载之后给文件设自定义属性(dead property)的客户端——davfs2、
// Cyberduck、Windows 资源管理器都会——每发一次 PROPPATCH,桶里那个对象
// 就变成 0 字节。客户端收到的还是一个正常的 207,数据已经没了。本机文件
// 没这毛病。
//
// 根因:x/net/webdav 的 patch()(prop.go)用光秃秃的 os.O_RDWR 打开资源,
// 只为了看它实现没实现 DeadPropsHolder,一个字节都不会写。DavFS.OpenFile
// 却把 O_RDWR 当成"要写",返回的写句柄一构造就挂着一个等 body 的
// PutObject;patch 的 defer f.Close() 一执行,空 body 就提交上去,原对象
// 被覆盖。
//
// 判据是 O_TRUNC/O_CREATE:真要写内容的三处调用(PUT、COPY 的目标、LOCK
// 给不存在的资源建空占位)都带,PROPPATCH 不带。所以这里的用例是成对的
// ——PROPPATCH 一个字节都不许写,PUT 和 LOCK 照旧要写。

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// deadPropPatch 是 Windows 资源管理器每次上传完都会补发的那组属性,三条
// 都在自定义命名空间里(DAV: 里的 live property 会被 x/net 提前挡掉,走
// 不到打开文件那一步,也就触发不了这个 bug)。
const deadPropPatch = `<?xml version="1.0" encoding="utf-8" ?>
<D:propertyupdate xmlns:D="DAV:" xmlns:Z="urn:schemas-microsoft-com:">
  <D:set>
    <D:prop>
      <Z:Win32CreationTime>Fri, 21 Aug 2026 09:00:00 GMT</Z:Win32CreationTime>
      <Z:Win32LastModifiedTime>Fri, 21 Aug 2026 09:00:00 GMT</Z:Win32LastModifiedTime>
      <Z:Win32FileAttributes>00000020</Z:Win32FileAttributes>
    </D:prop>
  </D:set>
</D:propertyupdate>`

// davReq 把一个请求打进 WebDAV handler,拿回应答。
func davReq(t *testing.T, h http.Handler, method, p, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, p, strings.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// 主症状。判据是"桶上有没有收到写请求":假 S3 不真的存对象,但对着一个
// 已有的 key 发出去的那一下,在真桶上就是覆盖。
func TestProppatchDoesNotWriteToBucket(t *testing.T) {
	// 目录也来一遍。S3 没有真目录,写句柄会照着 "music" 这个 key 建一个
	// 不带斜杠的 0 字节对象;之后 Stat 先命中它,那个文件夹就变成一个空
	// 文件,底下的东西整个看不见了。
	for _, tc := range []struct{ name, path string }{
		{"文件", "/dav/@" + fakeMount + "/music/a.mp3"},
		{"目录", "/dav/@" + fakeMount + "/music"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, fake, _ := newFakeDav(t, []string{"music/a.mp3"})

			w := davReq(t, h, "PROPPATCH", tc.path, deadPropPatch)

			// 修好之前:这里是 3 条(CreateMultipartUpload / 0 字节的
			// UploadPart / CompleteMultipartUpload)。
			if got := fake.writes(); len(got) != 0 {
				t.Errorf("PROPPATCH 往桶里发了 %d 个写请求,期望 0——内容被覆盖了:\n  %s",
					len(got), strings.Join(got, "\n  "))
			}
			if w.Code != http.StatusMultiStatus {
				t.Fatalf("PROPPATCH → %d, want 207: %s", w.Code, w.Body.String())
			}
			// 不支持 dead property 就照实说(和本机文件一个待遇),别回
			// 500:客户端把 500 当成整台服务器有毛病,把 403 当成"这个属性
			// 存不了",后者才是实情。
			body := w.Body.String()
			if !strings.Contains(body, "403") {
				t.Errorf("没有逐条给出 403,客户端会以为属性存下了:\n%s", body)
			}
			for _, name := range []string{"Win32CreationTime", "Win32LastModifiedTime", "Win32FileAttributes"} {
				if !strings.Contains(body, name) {
					t.Errorf("应答里没提到 %s:\n%s", name, body)
				}
			}
		})
	}
}

// 本机文件走的是 os.Root,O_RDWR 打开本来就不会截断——外部存储得和它
// 表现一致。同一份请求打两条路径,状态码必须一样。
func TestProppatchMatchesLocalBehaviour(t *testing.T) {
	h, _, _ := newFakeDav(t, []string{"music/a.mp3"})

	if w := davReq(t, h, http.MethodPut, "/dav/local.txt", "本机文件"); w.Code != http.StatusCreated {
		t.Fatalf("准备本机文件失败: %d %s", w.Code, w.Body.String())
	}
	local := davReq(t, h, "PROPPATCH", "/dav/local.txt", deadPropPatch)
	remote := davReq(t, h, "PROPPATCH", "/dav/@"+fakeMount+"/music/a.mp3", deadPropPatch)

	if local.Code != remote.Code {
		t.Errorf("本机文件 → %d,外部存储 → %d,两条路径应当一致\n本机: %s\n存储: %s",
			local.Code, remote.Code, local.Body.String(), remote.Body.String())
	}
}

// 反向:别把写分支一起关掉了。PUT 必须照旧把内容传上去。
func TestPutStillUploadsToBucket(t *testing.T) {
	const p = "/dav/@" + fakeMount + "/music/new.mp3"
	h, fake, _ := newFakeDav(t, nil)
	const content = "上传的内容"

	w := davReq(t, h, http.MethodPut, p, content)

	if w.Code != http.StatusCreated {
		t.Fatalf("PUT → %d, want 201: %s", w.Code, w.Body.String())
	}
	writes := fake.writes()
	if len(writes) == 0 {
		t.Fatal("PUT 没往桶里写任何东西")
	}
	want := fmt.Sprintf("body %d 字节", len(content))
	if !slices.ContainsFunc(writes, func(s string) bool { return strings.Contains(s, want) }) {
		t.Errorf("没看到 %d 字节的分片上传,桶上收到的是:\n  %s",
			len(content), strings.Join(writes, "\n  "))
	}
}

// LOCK 给不存在的资源建 0 字节占位是 x/net/webdav 有意为之(webdav.go),
// 客户端靠它先锁后传。这一下也走写分支,不能被一起挡掉。
func TestLockStillCreatesPlaceholder(t *testing.T) {
	const p = "/dav/@" + fakeMount + "/music/locked.mp3"
	h, fake, _ := newFakeDav(t, nil)
	const lockBody = `<?xml version="1.0" encoding="utf-8" ?>
<D:lockinfo xmlns:D="DAV:">
  <D:lockscope><D:exclusive/></D:lockscope>
  <D:locktype><D:write/></D:locktype>
</D:lockinfo>`

	w := davReq(t, h, "LOCK", p, lockBody)

	if w.Code != http.StatusCreated {
		t.Fatalf("LOCK → %d, want 201: %s", w.Code, w.Body.String())
	}
	if len(fake.writes()) == 0 {
		t.Error("LOCK 没在桶上建出占位对象,先锁后传的客户端会拿不到这个文件")
	}
}
