package cloud

import "testing"

// Region 曾经被无条件填成 "auto",导致 MinIO/AWS 签名校验失败
// ("the region is wrong")。只有 R2 需要 auto,其余留空让 SDK 自己探测。
func TestNormalizeRegion(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		region   string
		want     string
	}{
		{"R2 留空补 auto", "https://acct.r2.cloudflarestorage.com", "", "auto"},
		{"R2 无协议前缀也认得", "acct.r2.cloudflarestorage.com", "", "auto"},
		{"MinIO 留空保持空", "https://play.min.io", "", ""},
		{"AWS 留空保持空", "https://s3.amazonaws.com", "", ""},
		{"显式 region 不被覆盖", "https://s3.amazonaws.com", "us-west-2", "us-west-2"},
		{"R2 显式 region 不被覆盖", "https://acct.r2.cloudflarestorage.com", "wnam", "wnam"},
		// 桶名里带 r2.cloudflarestorage.com 的第三方域名不该误判
		{"相似域名不误判", "https://not-r2.cloudflarestorage.com.evil.test", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := policyReq{Name: "T", Endpoint: c.endpoint, Region: c.region,
				Bucket: "b", AccessKey: "k", SecretKey: "s"}
			if err := req.normalize(); err != nil {
				t.Fatalf("normalize: %v", err)
			}
			if req.Region != c.want {
				t.Errorf("Region = %q, want %q", req.Region, c.want)
			}
		})
	}
}

func TestNormalizeValidation(t *testing.T) {
	base := func() policyReq {
		return policyReq{Name: "R2", Endpoint: "https://x.example.com",
			Bucket: "b", AccessKey: "k", SecretKey: "s"}
	}

	t.Run("补全 https", func(t *testing.T) {
		req := base()
		req.Endpoint = "x.example.com"
		if err := req.normalize(); err != nil {
			t.Fatalf("normalize: %v", err)
		}
		if req.Endpoint != "https://x.example.com" {
			t.Errorf("Endpoint = %q", req.Endpoint)
		}
	})

	t.Run("保留 http", func(t *testing.T) {
		req := base()
		req.Endpoint = "http://192.168.1.9:9000" // 局域网自建 MinIO
		if err := req.normalize(); err != nil {
			t.Fatalf("normalize: %v", err)
		}
		if req.Endpoint != "http://192.168.1.9:9000" {
			t.Errorf("Endpoint = %q, http 被改写了", req.Endpoint)
		}
	})

	t.Run("basePath 去斜杠并统一分隔符", func(t *testing.T) {
		req := base()
		req.BasePath = `\pd\sub\`
		if err := req.normalize(); err != nil {
			t.Fatalf("normalize: %v", err)
		}
		if req.BasePath != "pd/sub" {
			t.Errorf("BasePath = %q, want pd/sub", req.BasePath)
		}
	})

	bad := []struct {
		name string
		mut  func(*policyReq)
	}{
		{"空名称", func(r *policyReq) { r.Name = "" }},
		{"名称带斜杠", func(r *policyReq) { r.Name = "a/b" }},
		{"名称带空格", func(r *policyReq) { r.Name = "my drive" }},
		{"名称超长", func(r *policyReq) { r.Name = "0123456789012345678901234567890123" }},
		{"空 endpoint", func(r *policyReq) { r.Endpoint = "" }},
		{"空 bucket", func(r *policyReq) { r.Bucket = "" }},
		{"空 accessKey", func(r *policyReq) { r.AccessKey = "" }},
	}
	for _, c := range bad {
		t.Run(c.name+"应报错", func(t *testing.T) {
			req := base()
			c.mut(&req)
			if err := req.normalize(); err == nil {
				t.Error("want error, got nil")
			}
		})
	}

	t.Run("中文名称合法", func(t *testing.T) {
		req := base()
		req.Name = "我的图床"
		if err := req.normalize(); err != nil {
			t.Errorf("中文挂载名应当合法: %v", err)
		}
	})
}
