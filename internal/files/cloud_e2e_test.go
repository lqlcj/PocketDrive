package files

// 网页端 API 打到外部存储的端到端:走真实 HTTP handler,覆盖分片上传
// (8MB 块 → S3 Multipart)、列目录、下载 302、改名/移动/写入。
// 凭据同 internal/cloud 的 e2e:
//
//	PD_S3_ENDPOINT=https://play.min.io PD_S3_BUCKET=<桶> \
//	PD_S3_KEY=... PD_S3_SECRET=... go test ./internal/files -run E2E -v

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pocketdrive/internal/cloud"
	"pocketdrive/internal/db"
)

// 与前端 Files.tsx 的 CHUNK_SIZE 保持一致;S3 要求非末片 ≥5MiB
const testChunk = 8 << 20

func testSvc(t *testing.T) (*Service, string) {
	t.Helper()
	ep, bucket := os.Getenv("PD_S3_ENDPOINT"), os.Getenv("PD_S3_BUCKET")
	key, secret := os.Getenv("PD_S3_KEY"), os.Getenv("PD_S3_SECRET")
	if ep == "" || bucket == "" || key == "" || secret == "" {
		t.Skip("未设置 PD_S3_* 环境变量,跳过外部存储端到端测试")
	}
	tmp := t.TempDir()
	gdb, err := db.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := gdb.DB(); err == nil {
			sqlDB.Close() // 不关 TempDir 删不掉 db 文件
		}
	})
	// 直接落库,不经 HandleSave——顺带验证从 DB 冷加载的挂载可用
	if err := gdb.Create(&db.StoragePolicy{
		Name: "E2E", Type: "s3", Endpoint: ep, Bucket: bucket,
		AccessKey: key, SecretKey: secret, BasePath: os.Getenv("PD_S3_BASE"),
	}).Error; err != nil {
		t.Fatalf("create policy: %v", err)
	}
	cs := cloud.New(gdb)
	if len(cs.Names()) != 1 {
		t.Fatalf("挂载未加载: %v", cs.Names())
	}
	svc, err := New(filepath.Join(tmp, "data"), filepath.Join(tmp, "uploads"), cs, gdb)
	if err != nil {
		t.Fatalf("files.New: %v", err)
	}
	// Windows 上 os.Root 持有目录句柄,不关就删不掉 TempDir
	t.Cleanup(func() { svc.Root().Close() })
	return svc, "@E2E"
}

// call 跑一个 handler,返回状态码与响应体。
func call(t *testing.T, h http.HandlerFunc, method, target string, body any) (int, []byte) {
	t.Helper()
	var req *http.Request
	if body == nil {
		req = httptest.NewRequest(method, target, nil)
	} else if r, ok := body.(*bytes.Reader); ok {
		req = httptest.NewRequest(method, target, r)
	} else {
		b, _ := json.Marshal(body)
		req = httptest.NewRequest(method, target, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h(w, req)
	return w.Code, w.Body.Bytes()
}

func mustCall(t *testing.T, h http.HandlerFunc, method, target string, body any) []byte {
	t.Helper()
	code, b := call(t, h, method, target, body)
	if code/100 != 2 {
		t.Fatalf("%s %s → %d: %s", method, target, code, b)
	}
	return b
}

type listResp struct {
	Path    string  `json:"path"`
	Entries []Entry `json:"entries"`
}

func listOf(t *testing.T, svc *Service, p string) listResp {
	t.Helper()
	b := mustCall(t, svc.HandleList, "GET", "/api/v1/files?path="+url.QueryEscape(p), nil)
	var lr listResp
	if err := json.Unmarshal(b, &lr); err != nil {
		t.Fatalf("解析列表: %v (%s)", err, b)
	}
	return lr
}

func TestE2ECloudAPI(t *testing.T) {
	svc, mnt := testSvc(t)

	root := fmt.Sprintf("%s/e2e-api-%d", mnt, time.Now().UnixNano())
	t.Cleanup(func() {
		m, rel, ok := svc.cloud.Resolve(strings.TrimPrefix(root, "/"))
		if !ok {
			return
		}
		ctx, cancel := contextTimeout()
		defer cancel()
		if err := m.Delete(ctx, rel); err != nil && !strings.Contains(err.Error(), "不存在") {
			t.Logf("清理失败(需手动删): %v", err)
		}
	})

	t.Run("根目录列出挂载点", func(t *testing.T) {
		lr := listOf(t, svc, "")
		if len(lr.Entries) == 0 || lr.Entries[0].Name != mnt || !lr.Entries[0].Dir {
			t.Fatalf("根目录首项应为挂载点 %s, got %+v", mnt, lr.Entries)
		}
	})

	t.Run("建目录", func(t *testing.T) {
		mustCall(t, svc.HandleMkdir, "POST", "/api/v1/files/mkdir",
			map[string]string{"path": root})
	})

	// 8MB + 1MB 两片,复刻前端大文件上传
	blob := make([]byte, testChunk+(1<<20))
	if _, err := rand.Read(blob); err != nil {
		t.Fatal(err)
	}
	bigPath := root + "/大文件.bin"

	t.Run("分片上传", func(t *testing.T) {
		b := mustCall(t, svc.HandleUploadInit, "POST", "/api/v1/files/upload/init",
			map[string]string{"path": bigPath})
		var initR struct {
			ID string `json:"id"`
		}
		json.Unmarshal(b, &initR)
		if !strings.HasPrefix(initR.ID, "s3") {
			t.Fatalf("外部存储应返回 s3 前缀会话 id, got %q", initR.ID)
		}

		for i, chunk := range [][]byte{blob[:testChunk], blob[testChunk:]} {
			mustCall(t, svc.HandleUploadChunk, "POST",
				fmt.Sprintf("/api/v1/files/upload/chunk?id=%s&index=%d", initR.ID, i),
				bytes.NewReader(chunk))
		}
		mustCall(t, svc.HandleUploadComplete, "POST", "/api/v1/files/upload/complete",
			map[string]any{"id": initR.ID, "path": bigPath, "chunks": 2})

		lr := listOf(t, svc, root)
		if len(lr.Entries) != 1 || lr.Entries[0].Name != "大文件.bin" {
			t.Fatalf("上传后列表 = %+v", lr.Entries)
		}
		if lr.Entries[0].Size != int64(len(blob)) {
			t.Errorf("size = %d, want %d", lr.Entries[0].Size, len(blob))
		}
	})

	t.Run("会话完成后失效", func(t *testing.T) {
		code, _ := call(t, svc.HandleUploadComplete, "POST", "/api/v1/files/upload/complete",
			map[string]any{"id": "s3" + strings.Repeat("0", 32), "path": bigPath, "chunks": 1})
		if code != http.StatusNotFound {
			t.Errorf("未知会话 → %d, want 404", code)
		}
	})

	t.Run("下载 302 到预签名", func(t *testing.T) {
		code, _ := call(t, svc.HandleDownload, "GET",
			"/api/v1/files/download?path="+url.QueryEscape(bigPath)+"&dl=1", nil)
		if code != http.StatusFound {
			t.Fatalf("下载 → %d, want 302", code)
		}
		req := httptest.NewRequest("GET",
			"/api/v1/files/download?path="+url.QueryEscape(bigPath)+"&dl=1", nil)
		w := httptest.NewRecorder()
		svc.HandleDownload(w, req)
		loc := w.Header().Get("Location")
		if loc == "" || !strings.Contains(loc, "X-Amz-Signature") {
			t.Fatalf("Location 不是预签名 URL: %q", loc)
		}
		// 真下回来比对字节,确认 302 目标可用
		resp, err := http.Get(loc)
		if err != nil {
			t.Fatalf("GET 预签名: %v", err)
		}
		defer resp.Body.Close()
		got := new(bytes.Buffer)
		got.ReadFrom(resp.Body)
		if !bytes.Equal(got.Bytes(), blob) {
			t.Errorf("下载内容与上传不一致 (%d vs %d 字节)", got.Len(), len(blob))
		}
	})

	t.Run("写入并读回文本", func(t *testing.T) {
		txt := root + "/说明.txt"
		const content = "外部存储的在线编辑\n"
		mustCall(t, svc.HandleWrite, "POST", "/api/v1/files/write",
			map[string]string{"path": txt, "content": content})
		b := mustCall(t, svc.HandleContent, "GET",
			"/api/v1/files/content?path="+url.QueryEscape(txt), nil)
		if string(b) != content {
			t.Errorf("读回 = %q, want %q", b, content)
		}
	})

	t.Run("改名", func(t *testing.T) {
		mustCall(t, svc.HandleRename, "POST", "/api/v1/files/rename",
			map[string]string{"path": root + "/说明.txt", "newName": "readme.txt"})
		lr := listOf(t, svc, root)
		var found bool
		for _, e := range lr.Entries {
			if e.Name == "readme.txt" {
				found = true
			}
			if e.Name == "说明.txt" {
				t.Error("旧名仍在")
			}
		}
		if !found {
			t.Errorf("改名后没找到 readme.txt: %+v", lr.Entries)
		}
	})

	t.Run("同挂载内移动", func(t *testing.T) {
		sub := root + "/子目录"
		mustCall(t, svc.HandleMkdir, "POST", "/api/v1/files/mkdir",
			map[string]string{"path": sub})
		mustCall(t, svc.HandleMove, "POST", "/api/v1/files/move",
			map[string]any{"path": root + "/readme.txt", "dest": sub})
		lr := listOf(t, svc, sub)
		if len(lr.Entries) != 1 || lr.Entries[0].Name != "readme.txt" {
			t.Fatalf("移动后子目录 = %+v", lr.Entries)
		}
	})

	t.Run("外部存储断点续传", func(t *testing.T) {
		// 复刻「传了一半断线」:第二次 init 必须找回同一个 S3 分片上传,
		// 并通过 ListParts 报出已传的分片
		blob2 := make([]byte, testChunk+2048)
		if _, err := rand.Read(blob2); err != nil {
			t.Fatal(err)
		}
		p := root + "/续传.bin"
		first := doInit(t, svc, p, int64(len(blob2)), 1785810000000, testChunk)
		if first.ID == "" || !strings.HasPrefix(first.ID, "s3") {
			t.Fatalf("会话 id = %q", first.ID)
		}
		putChunk(t, svc, first.ID, 0, blob2[:testChunk])

		again := doInit(t, svc, p, int64(len(blob2)), 1785810000000, testChunk)
		if again.ID != first.ID {
			t.Fatalf("会话 id 变了:%s → %s", first.ID, again.ID)
		}
		if len(again.Uploaded) != 1 || again.Uploaded[0] != 0 {
			t.Fatalf("已传分片 = %v, want [0]", again.Uploaded)
		}

		// 补完并合并——ETag 全部来自 ListParts,内存里没缓存任何分片
		putChunk(t, svc, first.ID, 1, blob2[testChunk:])
		mustCall(t, svc.HandleUploadComplete, "POST", "/api/v1/files/upload/complete",
			map[string]any{"id": first.ID, "path": p, "chunks": 2})

		lr := listOf(t, svc, root)
		var size int64
		for _, e := range lr.Entries {
			if e.Name == "续传.bin" {
				size = e.Size
			}
		}
		if size != int64(len(blob2)) {
			t.Fatalf("续传后大小 = %d, want %d", size, len(blob2))
		}
	})

	t.Run("跨存储移动被拒绝", func(t *testing.T) {
		code, body := call(t, svc.HandleMove, "POST", "/api/v1/files/move",
			map[string]any{"path": root + "/子目录/readme.txt", "dest": ""})
		if code/100 == 2 {
			t.Error("外部存储 → 本机的移动应当被拒绝")
		}
		if !strings.Contains(string(body), "跨存储") {
			t.Errorf("错误信息应说明原因, got %s", body)
		}
	})

	t.Run("未挂载的名字返回 404", func(t *testing.T) {
		code, _ := call(t, svc.HandleList, "GET", "/api/v1/files?path=%40NoSuch", nil)
		if code != http.StatusNotFound {
			t.Errorf("→ %d, want 404", code)
		}
	})
}
