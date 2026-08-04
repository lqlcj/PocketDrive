package share

// 外部存储分享的端到端:建分享 → 直链 302 → 真下回内容。
// 凭据同 internal/cloud 的 e2e(PD_S3_* 环境变量),未设置则跳过。

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pocketdrive/internal/cloud"
	"pocketdrive/internal/db"
	"pocketdrive/internal/files"
	"pocketdrive/internal/thumbs"
)

func testSvc(t *testing.T) (*Service, *cloud.Service) {
	t.Helper()
	ep, bucket := os.Getenv("PD_S3_ENDPOINT"), os.Getenv("PD_S3_BUCKET")
	key, secret := os.Getenv("PD_S3_KEY"), os.Getenv("PD_S3_SECRET")
	if ep == "" || bucket == "" || key == "" || secret == "" {
		t.Skip("未设置 PD_S3_* 环境变量,跳过外部存储分享测试")
	}
	tmp := t.TempDir()
	gdb, err := db.Open(filepath.Join(tmp, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := gdb.DB(); err == nil {
			sqlDB.Close()
		}
	})
	if err := gdb.Create(&db.StoragePolicy{
		Name: "E2E", Type: "s3", Endpoint: ep, Bucket: bucket,
		AccessKey: key, SecretKey: secret, BasePath: os.Getenv("PD_S3_BASE"),
	}).Error; err != nil {
		t.Fatalf("create policy: %v", err)
	}
	cs := cloud.New(gdb)
	fs, err := files.New(filepath.Join(tmp, "data"), filepath.Join(tmp, "uploads"), cs)
	if err != nil {
		t.Fatalf("files.New: %v", err)
	}
	t.Cleanup(func() { fs.Root().Close() })
	return New(gdb, fs, thumbs.New(fs, filepath.Join(tmp, "thumbs")), cs), cs
}

func TestE2ECloudShare(t *testing.T) {
	svc, cs := testSvc(t)

	m, _, ok := cs.Resolve("@E2E")
	if !ok {
		t.Fatal("挂载未加载")
	}
	ctx, cancel := ctxTimeout()
	defer cancel()

	root := fmt.Sprintf("e2e-share-%d", time.Now().UnixNano())
	const body = "分享给朋友的文件\n"
	rel := root + "/歌 song.txt"
	if err := m.Put(ctx, rel, strings.NewReader(body), int64(len(body))); err != nil {
		t.Fatalf("准备文件: %v", err)
	}
	t.Cleanup(func() {
		c, cancel := ctxTimeout()
		defer cancel()
		if err := m.Delete(c, root); err != nil {
			t.Logf("清理失败(需手动删): %v", err)
		}
	})

	sharePath := "@E2E/" + rel

	t.Run("建直链分享", func(t *testing.T) {
		sh, err := svc.Create(sharePath, "", "direct", 0)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if sh.Token == "" {
			t.Fatal("没拿到 token")
		}

		req := httptest.NewRequest("GET", "/d/"+sh.Token, nil)
		req.SetPathValue("token", sh.Token)
		w := httptest.NewRecorder()
		svc.HandleDirect(w, req)
		if w.Code != http.StatusFound {
			t.Fatalf("直链 → %d, want 302: %s", w.Code, w.Body)
		}
		loc := w.Header().Get("Location")
		if !strings.Contains(loc, "X-Amz-Signature") {
			t.Fatalf("Location 不是预签名 URL: %q", loc)
		}
		resp, err := http.Get(loc)
		if err != nil {
			t.Fatalf("GET 预签名: %v", err)
		}
		defer resp.Body.Close()
		got, _ := io.ReadAll(resp.Body)
		if string(got) != body {
			t.Errorf("下载内容 = %q, want %q", got, body)
		}
	})

	t.Run("分享信息返回真实大小", func(t *testing.T) {
		sh, err := svc.Create(sharePath, "", "page", 0)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		req := httptest.NewRequest("GET", "/api/v1/s/"+sh.Token, nil)
		req.SetPathValue("token", sh.Token)
		w := httptest.NewRecorder()
		svc.HandleInfo(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("info → %d: %s", w.Code, w.Body)
		}
		got := w.Body.String()
		if !strings.Contains(got, fmt.Sprintf(`"size":%d`, len(body))) {
			t.Errorf("info 没返回正确大小: %s", got)
		}
		if !strings.Contains(got, `"name":"歌 song.txt"`) {
			t.Errorf("info 文件名不对: %s", got)
		}
	})

	t.Run("外部存储不给缩略图", func(t *testing.T) {
		sh, _ := svc.Create(sharePath, "", "page", 0)
		req := httptest.NewRequest("GET", "/api/v1/s/"+sh.Token+"/thumb", nil)
		req.SetPathValue("token", sh.Token)
		w := httptest.NewRecorder()
		svc.HandleThumb(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("缩略图 → %d, want 404", w.Code)
		}
	})

	t.Run("不能分享挂载点本身", func(t *testing.T) {
		if _, err := svc.Create("@E2E", "", "page", 0); err == nil {
			t.Error("应当报错")
		}
	})

	t.Run("不能分享文件夹", func(t *testing.T) {
		if _, err := svc.Create("@E2E/"+root, "", "page", 0); err == nil {
			t.Error("分享文件夹应当报错")
		}
	})

	t.Run("未挂载的存储报错", func(t *testing.T) {
		if _, err := svc.Create("@NoSuch/a.txt", "", "page", 0); err == nil {
			t.Error("应当报错")
		}
	})

	t.Run("文件删除后分享失效", func(t *testing.T) {
		tmpRel := root + "/临时.txt"
		if err := m.Put(ctx, tmpRel, strings.NewReader("x"), 1); err != nil {
			t.Fatal(err)
		}
		sh, err := svc.Create("@E2E/"+tmpRel, "", "direct", 0)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := m.Delete(ctx, tmpRel); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("GET", "/d/"+sh.Token, nil)
		req.SetPathValue("token", sh.Token)
		w := httptest.NewRecorder()
		svc.HandleDirect(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("删除后直链 → %d, want 404", w.Code)
		}
	})
}
