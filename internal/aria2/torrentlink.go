package aria2

// .torrent 链接 → .torrent 文件:在输入框里粘贴 http(s) 的 .torrent 直链
// 直接交给 aria2.addUri,只会被当成普通文件下载——元数据解析不出来、
// 「选完再下」的文件清单也永远等不到(上传种子却一直好用)。
//
// 这里在后台把链接转成种子文件:下载、校验是 bencode 字典,再走与
// 「上传种子」完全相同的 AddTorrent 熟路把元数据接进来(转换骨架
// startConvert 见 magnet.go,去重与失败退避与磁力共用一套 job)。

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/anacrolix/torrent/metainfo"
)

// torrentLinkTimeout 单个种子链接的下载时限。.torrent 通常几十 ~ 几百 KB,
// 60 秒足够;超时/失败后按 magnetRetryGap 退避,前端「重试」可重新拉起。
const torrentLinkTimeout = 60 * time.Second

// torrentLinkMaxSize 种子文件大小上限,与 HandleAddTorrent 的 16MB 一致。
const torrentLinkMaxSize = 16 << 20

// isTorrentLink 判断 http(s) 直链是否指向 .torrent 文件:看 URL 路径
// 后缀,带 querystring(如 ?token=...)的链接也算。
func isTorrentLink(raw string) bool {
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".torrent")
}

// startTorrentLinkConvert 把 .torrent 链接的后台转换拉起:下载链接内容 →
// 校验 → AddTorrent。job 去重与退避复用 startConvert。
func (m *Manager) startTorrentLinkConvert(gid, link, relDir string) {
	m.startConvert(gid, link, relDir, func() (string, string, error) {
		return downloadTorrent(link)
	})
}

// downloadTorrent 请求 .torrent 链接拿回种子内容:校验是 bencode 字典
// (与 AddTorrent 的准入一致),转 base64 供 aria2.addTorrent,并顺便抠出
// 种子真名(添加后立刻就能展示,不用等 aria2 慢慢同步)。
func downloadTorrent(link string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), torrentLinkTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return "", "", errors.New("链接格式不正确")
	}
	req.Header.Set("User-Agent", "PocketDrive/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", errors.New("下载种子文件失败: " + err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("种子链接返回 %s", resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, torrentLinkMaxSize))
	if err != nil {
		return "", "", errors.New("下载种子文件失败: " + err.Error())
	}
	if len(raw) == 0 {
		return "", "", errors.New("种子链接返回了空文件")
	}
	if len(raw) == torrentLinkMaxSize {
		return "", "", errors.New("种子文件超过 16MB 上限")
	}
	// 与 AddTorrent 相同的白名单校验:bencode 字典以 'd' 开头
	if raw[0] != 'd' {
		return "", "", errors.New("链接内容不是 .torrent 文件")
	}

	name := linkBase(link)
	if mi, err := metainfo.Load(bytes.NewReader(raw)); err == nil {
		if info, err := mi.UnmarshalInfo(); err == nil && info.Name != "" {
			name = info.Name
		}
	}
	return base64.StdEncoding.EncodeToString(raw), name, nil
}

// linkBase 从链接尾巴猜一个名字(带 querystring 先剥掉),aria2 同步到
// bittorrent.info.name 后会用真名盖掉。
func linkBase(link string) string {
	name := path.Base(strings.TrimSpace(strings.SplitN(link, "?", 2)[0]))
	if name == "" || name == "." {
		name = "upload.torrent"
	}
	return name
}