package cloud

// 存储策略的端到端测试:打真实的 S3 兼容服务,覆盖列目录/上传/下载/
// 预签名/改名/分片/删除。默认跳过,设了凭据才跑:
//
//	PD_S3_ENDPOINT=https://play.min.io \
//	PD_S3_BUCKET=<桶名> \
//	PD_S3_KEY=... PD_S3_SECRET=... \
//	go test ./internal/cloud -run E2E -v
//
// play.min.io 是 MinIO 官方公共演示服(凭据公开、对象定期清理),
// 适合当一次性验证环境;换 R2/S3 只需改上面四个变量。

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	"pocketdrive/internal/db"
)

func testMount(t *testing.T) *S3Mount {
	t.Helper()
	ep, bucket := os.Getenv("PD_S3_ENDPOINT"), os.Getenv("PD_S3_BUCKET")
	key, secret := os.Getenv("PD_S3_KEY"), os.Getenv("PD_S3_SECRET")
	if ep == "" || bucket == "" || key == "" || secret == "" {
		t.Skip("未设置 PD_S3_* 环境变量,跳过外部存储端到端测试")
	}
	// 走一遍真实的入参清洗,顺带验证 normalize 的补全逻辑
	req := policyReq{Name: "E2E", Endpoint: ep, Bucket: bucket, AccessKey: key, SecretKey: secret,
		BasePath: os.Getenv("PD_S3_BASE")}
	if err := req.normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	m, err := newS3Mount(&db.StoragePolicy{
		Name: req.Name, Endpoint: req.Endpoint, Region: req.Region, Bucket: req.Bucket,
		AccessKey: req.AccessKey, SecretKey: req.SecretKey, BasePath: req.BasePath,
	})
	if err != nil {
		t.Fatalf("newS3Mount: %v", err)
	}
	return m
}

// names 把目录项压成 "name" / "name/"(目录)的有序集合,便于断言。
func names(es []Entry) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		if e.Dir {
			out = append(out, e.Name+"/")
		} else {
			out = append(out, e.Name)
		}
	}
	sort.Strings(out)
	return out
}

func TestE2E(t *testing.T) {
	m := testMount(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := m.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	// 每次跑用独立根目录,互不干扰;结束后整棵删掉
	root := fmt.Sprintf("e2e-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := m.Delete(c, root); err != nil {
			t.Logf("清理 %s 失败(需手动删): %v", root, err)
		}
	})

	const body = "PocketDrive 外部存储端到端测试\n"
	const fileName = "笔记 note.txt" // 中文+空格,验证 key 编码与预签名文件名

	t.Run("Mkdir+List", func(t *testing.T) {
		if err := m.Mkdir(ctx, root); err != nil {
			t.Fatalf("Mkdir root: %v", err)
		}
		if err := m.Mkdir(ctx, root+"/子目录"); err != nil {
			t.Fatalf("Mkdir sub: %v", err)
		}
		es, err := m.List(ctx, root)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		// 目录标记对象不能既当目录、又冒出一个同名空文件
		if got := names(es); len(got) != 1 || got[0] != "子目录/" {
			t.Fatalf("List = %v, want [子目录/]", got)
		}
	})

	t.Run("Put+Stat+Open", func(t *testing.T) {
		rel := root + "/" + fileName
		if err := m.Put(ctx, rel, strings.NewReader(body), int64(len(body))); err != nil {
			t.Fatalf("Put: %v", err)
		}
		st, err := m.Stat(ctx, rel)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if st.Dir || st.Size != int64(len(body)) || st.Name != fileName {
			t.Fatalf("Stat = %+v, want 文件 size=%d name=%q", st, len(body), fileName)
		}
		if st.Mtime <= 0 {
			t.Errorf("Stat.Mtime = %d, 前端要靠它显示时间", st.Mtime)
		}

		obj, e, err := m.Open(ctx, rel)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer obj.Close()
		got, err := io.ReadAll(obj)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if string(got) != body {
			t.Fatalf("内容 = %q, want %q", got, body)
		}
		if e.Size != int64(len(body)) {
			t.Errorf("Open entry size = %d, want %d", e.Size, len(body))
		}

		// Range 读:ServeContent 靠它做视频拖动
		if _, err := obj.Seek(4, io.SeekStart); err != nil {
			t.Fatalf("Seek: %v", err)
		}
		part := make([]byte, 6)
		if _, err := io.ReadFull(obj, part); err != nil {
			t.Fatalf("Range 读: %v", err)
		}
		if want := body[4:10]; string(part) != want {
			t.Errorf("Range = %q, want %q", part, want)
		}
	})

	t.Run("目录里能看到文件", func(t *testing.T) {
		es, err := m.List(ctx, root)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		want := []string{fileName, "子目录/"}
		sort.Strings(want) // names() 已排序,want 同样排序后比集合
		got := names(es)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Fatalf("List = %v, want %v", got, want)
		}
		// 目录必须排在文件前(与本机策略一致)
		if !es[0].Dir {
			t.Errorf("目录未排在最前: %v", got)
		}
	})

	t.Run("预签名直链", func(t *testing.T) {
		u, err := m.PresignGet(ctx, root+"/"+fileName, fileName, true)
		if err != nil {
			t.Fatalf("PresignGet: %v", err)
		}
		resp, err := http.Get(u)
		if err != nil {
			t.Fatalf("GET 预签名: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("预签名返回 %d", resp.StatusCode)
		}
		got, _ := io.ReadAll(resp.Body)
		if string(got) != body {
			t.Fatalf("预签名内容 = %q, want %q", got, body)
		}
		// 浏览器靠它决定"另存为"的文件名
		if cd := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") ||
			!strings.Contains(cd, "UTF-8''") {
			t.Errorf("Content-Disposition = %q, want attachment + UTF-8 文件名", cd)
		}
	})

	t.Run("Rename 文件", func(t *testing.T) {
		src, dst := root+"/"+fileName, root+"/子目录/改名后.txt"
		if err := m.Rename(ctx, src, dst); err != nil {
			t.Fatalf("Rename: %v", err)
		}
		if _, err := m.Stat(ctx, src); err == nil {
			t.Error("源文件仍在")
		}
		st, err := m.Stat(ctx, dst)
		if err != nil {
			t.Fatalf("Stat 目标: %v", err)
		}
		if st.Size != int64(len(body)) {
			t.Errorf("改名后 size = %d, want %d", st.Size, len(body))
		}
	})

	t.Run("Rename 目录", func(t *testing.T) {
		if err := m.Rename(ctx, root+"/子目录", root+"/新目录"); err != nil {
			t.Fatalf("Rename dir: %v", err)
		}
		es, err := m.List(ctx, root+"/新目录")
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if got := names(es); len(got) != 1 || got[0] != "改名后.txt" {
			t.Fatalf("改名后目录内容 = %v, want [改名后.txt]", got)
		}
		if es2, _ := m.List(ctx, root); len(names(es2)) != 1 {
			t.Errorf("根下残留旧目录: %v", names(es2))
		}
	})

	t.Run("Rename 拒绝移入自身", func(t *testing.T) {
		if err := m.Rename(ctx, root+"/新目录", root+"/新目录/更深"); err == nil {
			t.Error("移入自身内部应当报错")
		}
	})

	t.Run("分片上传", func(t *testing.T) {
		// S3 规定非末片 ≥5MiB;两片走完 init→put→complete
		const partSize = 5 << 20
		buf := make([]byte, partSize+1024)
		if _, err := rand.Read(buf); err != nil {
			t.Fatalf("rand: %v", err)
		}
		rel := root + "/大文件.bin"

		uploadID, err := m.MultipartInit(ctx, rel)
		if err != nil {
			t.Fatalf("MultipartInit: %v", err)
		}
		var parts []minio.CompletePart
		for i, chunk := range [][]byte{buf[:partSize], buf[partSize:]} {
			p, err := m.MultipartPut(ctx, rel, uploadID, i+1,
				bytes.NewReader(chunk), int64(len(chunk)))
			if err != nil {
				m.MultipartAbort(ctx, rel, uploadID)
				t.Fatalf("MultipartPut 片%d: %v", i+1, err)
			}
			parts = append(parts, p)
		}
		if err := m.MultipartComplete(ctx, rel, uploadID, parts); err != nil {
			t.Fatalf("MultipartComplete: %v", err)
		}

		st, err := m.Stat(ctx, rel)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if st.Size != int64(len(buf)) {
			t.Fatalf("分片上传后 size = %d, want %d", st.Size, len(buf))
		}
		obj, _, err := m.Open(ctx, rel)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer obj.Close()
		got, err := io.ReadAll(obj)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if !bytes.Equal(got, buf) {
			t.Fatal("分片上传后内容与原始字节不一致")
		}
	})

	t.Run("分片 Abort", func(t *testing.T) {
		rel := root + "/弃用.bin"
		uploadID, err := m.MultipartInit(ctx, rel)
		if err != nil {
			t.Fatalf("MultipartInit: %v", err)
		}
		if err := m.MultipartAbort(ctx, rel, uploadID); err != nil {
			t.Fatalf("MultipartAbort: %v", err)
		}
		if _, err := m.Stat(ctx, rel); err == nil {
			t.Error("abort 后不该留下对象")
		}
	})

	t.Run("Delete 递归", func(t *testing.T) {
		if err := m.Delete(ctx, root+"/新目录"); err != nil {
			t.Fatalf("Delete dir: %v", err)
		}
		if _, err := m.Stat(ctx, root+"/新目录/改名后.txt"); err == nil {
			t.Error("目录删除后子文件仍在")
		}
		if _, err := m.Stat(ctx, root+"/新目录"); err == nil {
			t.Error("目录标记未删除")
		}
	})

	t.Run("Delete 不存在的路径报错", func(t *testing.T) {
		if err := m.Delete(ctx, root+"/根本没有这个"); err == nil {
			t.Error("删除不存在的路径应当报错")
		}
	})
}
