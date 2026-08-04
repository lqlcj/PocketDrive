package cloud

import (
	"context"
	"errors"
	"io"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"pocketdrive/internal/db"
)

// Entry 与文件列表 API 的条目形状保持一致。
type Entry struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Dir   bool   `json:"dir"`
	Mtime int64  `json:"mtime"`
}

// renameLimit 单次文件夹重命名/移动最多搬运的对象数(S3 没有原子
// rename,只能逐个 copy+delete,防止超大文件夹把请求拖死)。
const renameLimit = 1000

type S3Mount struct {
	Name   string
	client *minio.Client
	core   *minio.Core
	bucket string
	base   string // 桶内前缀,"" 或 "a/b"(无首尾斜杠)
}

func newS3Mount(p *db.StoragePolicy) (*S3Mount, error) {
	u, err := url.Parse(p.Endpoint)
	if err != nil || u.Host == "" {
		return nil, errors.New("endpoint 不是合法 URL")
	}
	c, err := minio.New(u.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(p.AccessKey, p.SecretKey, ""),
		Secure: u.Scheme != "http",
		Region: p.Region,
	})
	if err != nil {
		return nil, err
	}
	return &S3Mount{
		Name:   p.Name,
		client: c,
		core:   &minio.Core{Client: c},
		bucket: p.Bucket,
		base:   strings.Trim(p.BasePath, "/"),
	}, nil
}

// key 把挂载内相对路径转成桶内对象 key。
func (m *S3Mount) key(rel string) string {
	rel = strings.Trim(rel, "/")
	if m.base == "" {
		return rel
	}
	if rel == "" {
		return m.base
	}
	return m.base + "/" + rel
}

func (m *S3Mount) Ping(ctx context.Context) error {
	ok, err := m.client.BucketExists(ctx, m.bucket)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("桶不存在或无访问权限(检查 bucket 名与密钥权限)")
	}
	return nil
}

// List 列出目录(delimiter 模式):公共前缀 → 文件夹,对象 → 文件。
func (m *S3Mount) List(ctx context.Context, rel string) ([]Entry, error) {
	prefix := m.key(rel)
	if prefix != "" {
		prefix += "/"
	}
	var out []Entry
	for obj := range m.client.ListObjects(ctx, m.bucket, minio.ListObjectsOptions{
		Prefix: prefix,
	}) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		name := strings.TrimPrefix(obj.Key, prefix)
		if name == "" {
			continue // 目录标记对象本身
		}
		if strings.HasSuffix(name, "/") {
			out = append(out, Entry{Name: strings.TrimSuffix(name, "/"), Dir: true})
		} else {
			out = append(out, Entry{
				Name: name, Size: obj.Size,
				Mtime: obj.LastModified.UnixMilli(),
			})
		}
	}
	// 文件夹按名,文件按修改时间倒序(与本机策略一致)
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

// Stat 返回文件/目录信息;目录靠"有该前缀的对象"或目录标记判断。
func (m *S3Mount) Stat(ctx context.Context, rel string) (Entry, error) {
	if strings.Trim(rel, "/") == "" {
		return Entry{Name: m.Name, Dir: true}, nil
	}
	info, err := m.client.StatObject(ctx, m.bucket, m.key(rel), minio.StatObjectOptions{})
	if err == nil {
		return Entry{
			Name: path.Base(rel), Size: info.Size,
			Mtime: info.LastModified.UnixMilli(),
		}, nil
	}
	// 不是对象:看是否是"目录"
	prefix := m.key(rel) + "/"
	for obj := range m.client.ListObjects(ctx, m.bucket, minio.ListObjectsOptions{
		Prefix: prefix, MaxKeys: 1,
	}) {
		if obj.Err == nil {
			return Entry{Name: path.Base(rel), Dir: true}, nil
		}
	}
	return Entry{}, errors.New("文件不存在")
}

// Open 打开对象用于读取;*minio.Object 实现 io.ReadSeekCloser,
// 可直接交给 http.ServeContent 做 Range 流式。
func (m *S3Mount) Open(ctx context.Context, rel string) (*minio.Object, Entry, error) {
	obj, err := m.client.GetObject(ctx, m.bucket, m.key(rel), minio.GetObjectOptions{})
	if err != nil {
		return nil, Entry{}, err
	}
	info, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, Entry{}, errors.New("文件不存在")
	}
	return obj, Entry{
		Name: path.Base(rel), Size: info.Size,
		Mtime: info.LastModified.UnixMilli(),
	}, nil
}

// Put 流式上传;size 未知传 -1(minio 会按 PartSize 分片,内存占用
// 即 PartSize,为 1-2G 小内存 VPS 压到 8MB)。
func (m *S3Mount) Put(ctx context.Context, rel string, r io.Reader, size int64) error {
	_, err := m.client.PutObject(ctx, m.bucket, m.key(rel), r, size,
		minio.PutObjectOptions{PartSize: 8 << 20})
	return err
}

func (m *S3Mount) Mkdir(ctx context.Context, rel string) error {
	if strings.Trim(rel, "/") == "" {
		return errors.New("目录名不能为空")
	}
	_, err := m.client.PutObject(ctx, m.bucket, m.key(rel)+"/",
		strings.NewReader(""), 0, minio.PutObjectOptions{})
	return err
}

// listAll 递归列出 rel 下(含 rel 自身对象/目录标记)的全部对象 key。
func (m *S3Mount) listAll(ctx context.Context, rel string) ([]string, error) {
	var keys []string
	k := m.key(rel)
	// rel 自身如果是对象(文件或目录标记)也纳入
	for _, cand := range []string{k, k + "/"} {
		if _, err := m.client.StatObject(ctx, m.bucket, cand, minio.StatObjectOptions{}); err == nil {
			keys = append(keys, cand)
		}
	}
	for obj := range m.client.ListObjects(ctx, m.bucket, minio.ListObjectsOptions{
		Prefix: k + "/", Recursive: true,
	}) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		keys = append(keys, obj.Key)
	}
	return keys, nil
}

// Delete 删除文件或整个目录(外部存储不经回收站)。
func (m *S3Mount) Delete(ctx context.Context, rel string) error {
	keys, err := m.listAll(ctx, rel)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return errors.New("文件不存在")
	}
	for _, k := range keys {
		if err := m.client.RemoveObject(ctx, m.bucket, k, minio.RemoveObjectOptions{}); err != nil {
			return err
		}
	}
	return nil
}

func (m *S3Mount) copyOne(ctx context.Context, srcKey, dstKey string) error {
	_, err := m.client.CopyObject(ctx,
		minio.CopyDestOptions{Bucket: m.bucket, Object: dstKey},
		minio.CopySrcOptions{Bucket: m.bucket, Object: srcKey})
	return err
}

// Rename 同挂载内改名/移动(dstRel 为完整目标相对路径)。S3 无原子
// rename:文件 copy+delete;目录逐对象搬运,超过 renameLimit 拒绝。
func (m *S3Mount) Rename(ctx context.Context, srcRel, dstRel string) error {
	srcKey, dstKey := m.key(srcRel), m.key(dstRel)
	if srcKey == dstKey {
		return nil
	}
	if strings.HasPrefix(dstKey+"/", srcKey+"/") {
		return errors.New("不能移动到自身内部")
	}
	// 文件:单对象
	if _, err := m.client.StatObject(ctx, m.bucket, srcKey, minio.StatObjectOptions{}); err == nil {
		if err := m.copyOne(ctx, srcKey, dstKey); err != nil {
			return err
		}
		return m.client.RemoveObject(ctx, m.bucket, srcKey, minio.RemoveObjectOptions{})
	}
	// 目录:逐对象
	keys, err := m.listAll(ctx, srcRel)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return errors.New("文件不存在")
	}
	if len(keys) > renameLimit {
		return errors.New("文件夹内对象过多(>1000),外部存储暂不支持整体改名/移动")
	}
	for _, k := range keys {
		nk := dstKey + strings.TrimPrefix(k, srcKey)
		if err := m.copyOne(ctx, k, nk); err != nil {
			return err
		}
	}
	for _, k := range keys {
		if err := m.client.RemoveObject(ctx, m.bucket, k, minio.RemoveObjectOptions{}); err != nil {
			return err
		}
	}
	return nil
}

// PresignGet 生成预签名下载 URL;attachment 控制保存/内联,filename
// 保证保存名正确。播放器/浏览器直连存储桶,不过 VPS 中转。
func (m *S3Mount) PresignGet(ctx context.Context, rel, filename string, attachment bool) (string, error) {
	disp := "inline"
	if attachment {
		disp = "attachment"
	}
	params := url.Values{}
	params.Set("response-content-disposition",
		disp+"; filename*=UTF-8''"+url.PathEscape(filename))
	u, err := m.client.PresignedGetObject(ctx, m.bucket, m.key(rel), 4*time.Hour, params)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// ---- 分片上传(网页端大文件 → S3 Multipart) ----

func (m *S3Mount) MultipartInit(ctx context.Context, rel string) (string, error) {
	return m.core.NewMultipartUpload(ctx, m.bucket, m.key(rel), minio.PutObjectOptions{})
}

func (m *S3Mount) MultipartPut(ctx context.Context, rel, uploadID string, partNum int, r io.Reader, size int64) (minio.CompletePart, error) {
	p, err := m.core.PutObjectPart(ctx, m.bucket, m.key(rel), uploadID, partNum, r, size, minio.PutObjectPartOptions{})
	if err != nil {
		return minio.CompletePart{}, err
	}
	return minio.CompletePart{PartNumber: p.PartNumber, ETag: p.ETag}, nil
}

func (m *S3Mount) MultipartComplete(ctx context.Context, rel, uploadID string, parts []minio.CompletePart) error {
	_, err := m.core.CompleteMultipartUpload(ctx, m.bucket, m.key(rel), uploadID, parts, minio.PutObjectOptions{})
	return err
}

func (m *S3Mount) MultipartAbort(ctx context.Context, rel, uploadID string) error {
	return m.core.AbortMultipartUpload(ctx, m.bucket, m.key(rel), uploadID)
}
