package files

// 断点续传:传了一半中断,重新 init 同一个文件应当返回同一个会话和已传
// 分片列表,只补剩下的块。用本机存储验证,不需要网络。

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"pocketdrive/internal/cloud"
	"pocketdrive/internal/db"
)

func localSvc(t *testing.T) *Service {
	t.Helper()
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
	svc, err := New(filepath.Join(tmp, "data"), filepath.Join(tmp, "uploads"),
		cloud.New(gdb), gdb)
	if err != nil {
		t.Fatalf("files.New: %v", err)
	}
	t.Cleanup(func() { svc.Root().Close() })
	return svc
}

type initResp struct {
	ID        string `json:"id"`
	Uploaded  []int  `json:"uploaded"`
	ChunkSize int64  `json:"chunkSize"`
}

func doInit(t *testing.T, svc *Service, path string, size, mtime, chunk int64) initResp {
	t.Helper()
	b := mustCall(t, svc.HandleUploadInit, "POST", "/api/v1/files/upload/init",
		map[string]any{
			"path": path, "size": size, "lastModified": mtime, "chunkSize": chunk,
		})
	var r initResp
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("解析 init: %v (%s)", err, b)
	}
	return r
}

func putChunk(t *testing.T, svc *Service, id string, index int, data []byte) {
	t.Helper()
	mustCall(t, svc.HandleUploadChunk, "POST",
		fmt.Sprintf("/api/v1/files/upload/chunk?id=%s&index=%d", id, index),
		bytes.NewReader(data))
}

func TestUploadResume(t *testing.T) {
	svc := localSvc(t)
	const chunk = 1 << 10
	// 3 块:2 块整 + 1 块零头
	body := bytes.Repeat([]byte("A"), chunk*2+300)
	parts := [][]byte{body[:chunk], body[chunk : chunk*2], body[chunk*2:]}
	const target = "视频/大文件.bin"
	const mtime = 1785810000000

	first := doInit(t, svc, target, int64(len(body)), mtime, chunk)
	if first.ID == "" {
		t.Fatal("没拿到会话 id")
	}
	if len(first.Uploaded) != 0 {
		t.Fatalf("新会话不该有已传分片: %v", first.Uploaded)
	}
	if first.ChunkSize != chunk {
		t.Errorf("chunkSize = %d, want %d", first.ChunkSize, chunk)
	}

	// 传前两块后"断线"
	putChunk(t, svc, first.ID, 0, parts[0])
	putChunk(t, svc, first.ID, 1, parts[1])

	t.Run("重新 init 找回会话与进度", func(t *testing.T) {
		again := doInit(t, svc, target, int64(len(body)), mtime, chunk)
		if again.ID != first.ID {
			t.Fatalf("会话 id 变了:%s → %s,续传会从头开始", first.ID, again.ID)
		}
		if len(again.Uploaded) != 2 || again.Uploaded[0] != 0 || again.Uploaded[1] != 1 {
			t.Fatalf("已传分片 = %v, want [0 1]", again.Uploaded)
		}
	})

	t.Run("补最后一块并合并", func(t *testing.T) {
		putChunk(t, svc, first.ID, 2, parts[2])
		mustCall(t, svc.HandleUploadComplete, "POST", "/api/v1/files/upload/complete",
			map[string]any{"id": first.ID, "path": target, "chunks": 3})

		f, err := svc.Root().Open(target)
		if err != nil {
			t.Fatalf("打开结果: %v", err)
		}
		defer f.Close()
		got := new(bytes.Buffer)
		if _, err := got.ReadFrom(f); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Bytes(), body) {
			t.Fatalf("拼接结果不对:%d 字节, want %d", got.Len(), len(body))
		}
	})

	t.Run("完成后会话失效", func(t *testing.T) {
		code, _ := call(t, svc.HandleUploadComplete, "POST", "/api/v1/files/upload/complete",
			map[string]any{"id": first.ID, "path": target, "chunks": 3})
		if code != http.StatusNotFound {
			t.Errorf("→ %d, want 404", code)
		}
	})
}

// 文件本身变了就不能接着旧进度传,否则会拼出一个损坏的文件。
func TestUploadResumeRejectsChangedFile(t *testing.T) {
	svc := localSvc(t)
	const chunk = 1 << 10
	const target = "a.bin"

	base := doInit(t, svc, target, 4096, 1785810000000, chunk)
	putChunk(t, svc, base.ID, 0, bytes.Repeat([]byte("A"), chunk))

	cases := []struct {
		name                 string
		size, mtime, chunkSz int64
	}{
		{"大小变了", 8192, 1785810000000, chunk},
		{"修改时间变了", 4096, 1785899999999, chunk},
		{"分片大小变了", 4096, 1785810000000, chunk * 2},
	}
	for _, c := range cases {
		t.Run(c.name+"应当开新会话", func(t *testing.T) {
			got := doInit(t, svc, target, c.size, c.mtime, c.chunkSz)
			if got.ID == base.ID {
				t.Error("复用了旧会话,会拼出损坏的文件")
			}
			if len(got.Uploaded) != 0 {
				t.Errorf("新会话不该带已传分片: %v", got.Uploaded)
			}
		})
	}
}

// 写了一半的分片不能被算作"已传",否则续传会跳过它、拼出缺角的文件。
func TestUploadResumeIgnoresPartialChunk(t *testing.T) {
	svc := localSvc(t)
	const chunk = 1 << 10
	const target = "b.bin"

	r := doInit(t, svc, target, chunk*2, 1785810000000, chunk)
	putChunk(t, svc, r.ID, 0, bytes.Repeat([]byte("A"), chunk))
	// 模拟中断:残留一个 .partial 临时文件
	dir := filepath.Join(svc.tmpDir, r.ID)
	if err := os.WriteFile(filepath.Join(dir, "part_00001.partial"),
		[]byte("半块"), 0o644); err != nil {
		t.Fatal(err)
	}

	again := doInit(t, svc, target, chunk*2, 1785810000000, chunk)
	if len(again.Uploaded) != 1 || again.Uploaded[0] != 0 {
		t.Fatalf("已传分片 = %v, want [0](半块不算数)", again.Uploaded)
	}
}
