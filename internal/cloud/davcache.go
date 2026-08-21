package cloud

import (
	"context"
	"path"
	"strings"
	"sync"
)

// listCache 是单次 PROPFIND 内的目录列表缓存。
//
// x/net/webdav 的 walkFS 拿到 Readdir 的结果之后,把 FileInfo 整份丢掉,
// 再对每个孩子重新调一次 FileSystem.Stat(x/net webdav/file.go 的
// walkFS)。落到外部存储上就是一个文件一次 HEAD:一千首歌的文件夹 =
// 一千次串行往返,按 R2 单程 40ms 算就是 40 秒起步,手机客户端等不到就
// 一直转圈——顺带还是一千次 Class B 计费。davfs.go 里的 ContentType 挡
// 的是再往后一层的类型嗅探,遍历阶段就超时了,根本走不到那儿。
//
// 这里让 Readdir 顺手把整份列表挂到 ctx 上,紧接着那一千次 Stat 全部命
// 中内存,一次网络请求都不发。缓存只在 PROPFIND 期间存在(见
// internal/dav/dav.go):那是只读请求,不存在读到自己刚写的旧值。
type listCache struct {
	mu sync.Mutex
	m  map[string]Entry // "挂载名\x00相对路径" -> 条目
}

// listCacheMax 兜住 Depth: infinity 的客户端,别让一次请求把整个桶的条
// 目都堆进内存。超出就不再记,退回逐个 Stat——慢,但不会把进程撑爆。
const listCacheMax = 100_000

type listCacheKey struct{}

// WithListCache 给 ctx 挂一张空缓存表,随请求结束一起回收。
func WithListCache(ctx context.Context) context.Context {
	return context.WithValue(ctx, listCacheKey{}, &listCache{m: make(map[string]Entry)})
}

// listCacheFrom 取出 ctx 上的缓存;没挂就是 nil,下面的方法对 nil 安全,
// 调用方不用分支。
func listCacheFrom(ctx context.Context) *listCache {
	c, _ := ctx.Value(listCacheKey{}).(*listCache)
	return c
}

func cacheKeyOf(mount, rel string) string {
	return mount + "\x00" + strings.Trim(rel, "/")
}

func (c *listCache) get(mount, rel string) (Entry, bool) {
	if c == nil {
		return Entry{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[cacheKeyOf(mount, rel)]
	return e, ok
}

func (c *listCache) put(mount, rel string, e Entry) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= listCacheMax {
		return
	}
	c.m[cacheKeyOf(mount, rel)] = e
}

// putDir 把一次 List 的结果整份存进去,喂给紧随其后的那批 Stat。
func (c *listCache) putDir(mount, rel string, entries []Entry) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m)+len(entries) > listCacheMax {
		return
	}
	for _, e := range entries {
		c.m[cacheKeyOf(mount, path.Join(rel, e.Name))] = e
	}
}
