package dav

// WebDAV 直连的端到端测试:打真实的 S3 兼容服务,确认
//
//	开关开 → GET 拿到 302,Location 指向存储桶(不是本站),跟过去内容正确
//	开关关 → GET 由本站中转,直接返回内容
//
// 默认跳过,设了凭据才跑(变量与 internal/cloud 的 E2E 一致):
//
//	PD_S3_ENDPOINT=https://play.min.io \
//	PD_S3_BUCKET=<桶名> \
//	PD_S3_KEY=... PD_S3_SECRET=... \
//	go test ./internal/dav -run E2E -v

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pocketdrive/internal/cloud"
	"pocketdrive/internal/db"
)

const e2eMount = "E2E"

// e2ePolicy 从环境变量拼出策略。cloud 包的入参清洗是包内私有的,这里
// 补两条它做过的事:补协议头、R2 的 region 固定 auto。
func e2ePolicy(t *testing.T) *db.StoragePolicy {
	t.Helper()
	ep, bucket := os.Getenv("PD_S3_ENDPOINT"), os.Getenv("PD_S3_BUCKET")
	key, secret := os.Getenv("PD_S3_KEY"), os.Getenv("PD_S3_SECRET")
	if ep == "" || bucket == "" || key == "" || secret == "" {
		t.Skip("未设置 PD_S3_* 环境变量,跳过 WebDAV 直连端到端测试")
	}
	if !strings.HasPrefix(ep, "http://") && !strings.HasPrefix(ep, "https://") {
		ep = "https://" + ep
	}
	region := os.Getenv("PD_S3_REGION")
	if region == "" && strings.Contains(ep, ".r2.cloudflarestorage.com") {
		region = "auto"
	}
	return &db.StoragePolicy{
		Name: e2eMount, Type: "s3", Endpoint: ep, Region: region, Bucket: bucket,
		AccessKey: key, SecretKey: secret, BasePath: os.Getenv("PD_S3_BASE"),
	}
}

// setDavDirect 走真实的设置接口,顺带覆盖 HandleSaveSettings。
func setDavDirect(t *testing.T, svc *cloud.Service, on bool) {
	t.Helper()
	body := `{"davDirect":false}`
	if on {
		body = `{"davDirect":true}`
	}
	w := httptest.NewRecorder()
	svc.HandleSaveSettings(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("保存设置失败: %d %s", w.Code, w.Body.String())
	}
	if svc.DavDirect() != on {
		t.Fatalf("DavDirect() = %v, want %v", svc.DavDirect(), on)
	}
}

func TestE2EDavDirect(t *testing.T) {
	policy := e2ePolicy(t)
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
	if err := gdb.Create(policy).Error; err != nil {
		t.Fatalf("写入策略: %v", err)
	}

	dataDir := filepath.Join(dir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	svc := cloud.New(gdb)
	m, _, ok := svc.Resolve("@" + e2eMount)
	if !ok {
		t.Fatal("挂载没加载出来(策略配置有问题?)")
	}

	ctx := context.Background()
	const rel = "pd-dav-direct-e2e.txt"
	want := []byte("直连测试内容 direct-stream")
	if err := m.Put(ctx, rel, bytes.NewReader(want), int64(len(want))); err != nil {
		t.Fatalf("上传测试对象: %v", err)
	}
	t.Cleanup(func() { _ = m.Delete(context.Background(), rel) })

	srv := httptest.NewServer(Handler(dataDir, svc))
	t.Cleanup(srv.Close)
	davURL := srv.URL + "/dav/@" + e2eMount + "/" + rel

	// 不跟随重定向的客户端:用来看清 302 本身
	noFollow := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	t.Run("开关开则 302 到存储桶", func(t *testing.T) {
		setDavDirect(t, svc, true)
		resp, err := noFollow.Get(davURL)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("状态码 = %d, want 302", resp.StatusCode)
		}
		loc, err := url.Parse(resp.Header.Get("Location"))
		if err != nil {
			t.Fatalf("Location 不是合法 URL: %v", err)
		}
		if srvURL, _ := url.Parse(srv.URL); loc.Host == srvURL.Host {
			t.Fatalf("Location 还指着本站(%s),没有真的直连", loc.Host)
		}
		// 跟过去:预签名 URL 本身要能拿到原内容
		got, err := http.Get(loc.String())
		if err != nil {
			t.Fatalf("跟随重定向: %v", err)
		}
		defer got.Body.Close()
		if got.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(got.Body)
			t.Fatalf("桶上取回状态码 = %d, body=%s", got.StatusCode, b)
		}
		b, _ := io.ReadAll(got.Body)
		if !bytes.Equal(b, want) {
			t.Errorf("内容 = %q, want %q", b, want)
		}
	})

	t.Run("HEAD 始终本地应答", func(t *testing.T) {
		setDavDirect(t, svc, true)
		resp, err := noFollow.Head(davURL)
		if err != nil {
			t.Fatalf("HEAD: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("状态码 = %d, want 200", resp.StatusCode)
		}
		if resp.ContentLength != int64(len(want)) {
			t.Errorf("Content-Length = %d, want %d", resp.ContentLength, len(want))
		}
	})

	t.Run("开关关则本站中转", func(t *testing.T) {
		setDavDirect(t, svc, false)
		resp, err := noFollow.Get(davURL)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("状态码 = %d, want 200(中转)", resp.StatusCode)
		}
		b, _ := io.ReadAll(resp.Body)
		if !bytes.Equal(b, want) {
			t.Errorf("内容 = %q, want %q", b, want)
		}
	})

	t.Run("目录不重定向", func(t *testing.T) {
		setDavDirect(t, svc, true)
		resp, err := noFollow.Get(srv.URL + "/dav/@" + e2eMount)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusFound {
			t.Errorf("挂载点被重定向到 %q", resp.Header.Get("Location"))
		}
	})
}
