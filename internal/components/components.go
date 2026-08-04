// Package components 汇报 PocketDrive 依赖的三个外部程序(yt-dlp、
// aria2、ffmpeg)的状态,并负责其中可托管者的安装升级。
//
// 三者的升级渠道是有意分开的:
//
//	yt-dlp   装在 /config/bin(volume 内),网页里点一下就升级,结果
//	         跨容器重启保留。它一年发上百个版本,视频站点一改规则就
//	         得跟,跟着镜像走太慢。
//	aria2    跑在自己的容器里(compose 的 aria2 服务),版本通过它的
//	         RPC 查,升级靠 docker compose pull。
//	ffmpeg   随 PocketDrive 主镜像发布(Alpine 官方包),升级同样靠
//	         docker compose pull。
//
// 于是「下载二进制」这套逻辑只对 yt-dlp 存在,这个包里也只有它一份
// 安装代码:依赖第三方 release 的资产命名是有长期代价的(上游改个名
// 字,更新按钮就坏了),能少一处是一处。
//
// 本机开发(Windows/macOS)不下载任何东西:binDir 为空时一律回退到
// PATH 里的同名程序。
package components

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Kind 是组件标识。yt-dlp 和 ffmpeg 的取值同时也是可执行文件名。
type Kind string

const (
	Ytdlp  Kind = "yt-dlp"
	Aria2  Kind = "aria2"
	FFmpeg Kind = "ffmpeg"
)

// Channel 是组件的升级渠道,决定网页上能对它做什么。
type Channel string

const (
	// ChanManaged:装在 volume 里,网页里点一下就能升级。
	ChanManaged Channel = "managed"
	// ChanImage:随 PocketDrive 主镜像发布。
	ChanImage Channel = "image"
	// ChanSidecar:跑在自己的容器里。
	ChanSidecar Channel = "sidecar"
	// ChanSystem:本机开发,版本取决于用户 PATH 里装了什么。
	ChanSystem Channel = "system"
)

const (
	downloadTimeout = 10 * time.Minute
	versionTTL      = 10 * time.Minute
	latestTTL       = time.Hour // GitHub 未认证接口每小时 60 次,查勤了会被限流
)

// yt-dlp 的独立二进制现在 35MB 上下,给到 100MB 已经很宽松;撞上这个
// 数说明下到的不是它。是变量只为让测试能调小。
var maxDownload int64 = 100 << 20

// Info 是一个组件对外呈现的状态。
type Info struct {
	Kind      Kind    `json:"kind"`
	Installed bool    `json:"installed"`
	Version   string  `json:"version"`
	Latest    string  `json:"latest"`   // 上游最新版;只有托管组件才查
	Outdated  bool    `json:"outdated"` // 能比较且确实旧了
	Channel   Channel `json:"channel"`
	Path      string  `json:"path"`
}

type cached struct {
	value string
	at    time.Time
}

type Manager struct {
	binDir  string // 空 = 不托管,全部走 PATH
	bundled string // 镜像内置副本所在目录,可为空

	mu       sync.Mutex
	versions map[Kind]cached
	latests  map[Kind]cached
	// 同一组件的下载互斥,避免两次点击下到同一个文件上
	installing map[Kind]bool
}

func New(binDir, bundledDir string) *Manager {
	return &Manager{
		binDir: binDir, bundled: bundledDir,
		versions:   make(map[Kind]cached),
		latests:    make(map[Kind]cached),
		installing: make(map[Kind]bool),
	}
}

// Channel 报告组件的升级渠道。官方镜像一定会设 POCKETDRIVE_BIN_DIR,
// 所以没设就说明是本机开发,一切都看用户自己的 PATH。
func (m *Manager) Channel(k Kind) Channel {
	if m.binDir == "" {
		return ChanSystem
	}
	switch k {
	case Ytdlp:
		return ChanManaged
	case Aria2:
		return ChanSidecar
	default:
		return ChanImage
	}
}

// Managed 报告组件能不能在网页里升级。
func (m *Manager) Managed(k Kind) bool { return m.Channel(k) == ChanManaged }

// Path 返回组件的可执行文件路径。只有托管的 yt-dlp 在 volume 里,
// 其余交给 PATH 解析;aria2 压根不在本容器内,没有路径可言。
func (m *Manager) Path(k Kind) string {
	switch {
	case k == Aria2:
		return ""
	case k == Ytdlp && m.binDir != "":
		return filepath.Join(m.binDir, exeName(string(k)))
	default:
		return exeName(string(k))
	}
}

func exeName(n string) string {
	if runtime.GOOS == "windows" {
		return n + ".exe"
	}
	return n
}

// EnsureBundled 把镜像内置的副本复制进 volume。已存在的不覆盖——
// 那可能是用户在网页上升级过的、比镜像里更新的版本。
func (m *Manager) EnsureBundled(kinds ...Kind) {
	if m.binDir == "" || m.bundled == "" {
		return
	}
	for _, k := range kinds {
		if !m.Managed(k) {
			continue
		}
		dst := m.Path(k)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		src := filepath.Join(m.bundled, exeName(string(k)))
		f, err := os.Open(src)
		if err != nil {
			continue // 镜像没内置这个组件,等用户自己点下载
		}
		_ = writeExecutable(dst, f)
		f.Close()
	}
}

// writeExecutable 原子地写入一个可执行文件:先写临时文件再改名,
// 避免下载中断留下一个「存在但残缺」的二进制。
func writeExecutable(dst string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(r, maxDownload))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if n == 0 {
		os.Remove(tmp)
		return errors.New("下载到的文件是空的")
	}
	// 撞到上限说明下错了东西:LimitReader 是干净地读完的,io.Copy 不
	// 会报错,再往下走就会留下一个「能启动但缺后半截」的二进制
	if n >= maxDownload {
		os.Remove(tmp)
		return fmt.Errorf("下载内容超过 %d MB,已中止", maxDownload>>20)
	}
	// Windows 上目标被占用时改名会失败,先把旧的挪开
	if runtime.GOOS == "windows" {
		_ = os.Rename(dst, dst+".old")
		defer os.Remove(dst + ".old")
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Chmod(dst, 0o755)
}

// ---- 版本 ----

// Version 读取组件自报的版本号(带缓存)。未安装返回空串。
// aria2 不在本容器内,它的版本由调用方通过 RPC 取,这里始终返回空。
func (m *Manager) Version(k Kind) string {
	if k == Aria2 {
		return ""
	}
	m.mu.Lock()
	if c, ok := m.versions[k]; ok && time.Since(c.at) < versionTTL {
		m.mu.Unlock()
		return c.value
	}
	m.mu.Unlock()

	v := m.readVersion(k)
	m.mu.Lock()
	m.versions[k] = cached{v, time.Now()}
	m.mu.Unlock()
	return v
}

func (m *Manager) readVersion(k Kind) string {
	bin := m.Path(k)
	if _, err := exec.LookPath(bin); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, versionArg(k)).Output()
	if err != nil {
		return ""
	}
	return parseVersion(k, string(out))
}

func versionArg(k Kind) string {
	if k == Ytdlp {
		return "--version"
	}
	return "-version" // ffmpeg
}

// parseVersion 从各家五花八门的输出里抠出版本号。
func parseVersion(k Kind, out string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	line = strings.TrimSpace(line)
	switch k {
	case Ytdlp:
		return line // yt-dlp 直接打印 "2026.01.01"
	case FFmpeg:
		// "ffmpeg version n7.1-xxx Copyright (c) ..."
		if rest, ok := strings.CutPrefix(line, "ffmpeg version "); ok {
			v, _, _ := strings.Cut(rest, " ")
			return v
		}
	}
	return line
}

// invalidate 让下次 Version() 重新读(升级后调用)。
func (m *Manager) invalidate(k Kind) {
	m.mu.Lock()
	delete(m.versions, k)
	m.mu.Unlock()
}

// ---- 上游最新版 ----

type ghRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func fetchRelease(ctx context.Context, repo string) (*ghRelease, error) {
	url := "https://api.github.com/repos/" + repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		return nil, errors.New("GitHub 接口限流,请过一会再试")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub 返回 %d", resp.StatusCode)
	}
	var rel ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// Latest 查上游最新版本号(带缓存)。只有托管组件需要——其余的升级
// 不归网页管,查了也没处用。查不到返回空串。
func (m *Manager) Latest(ctx context.Context, k Kind) string {
	if !m.Managed(k) {
		return ""
	}
	m.mu.Lock()
	if c, ok := m.latests[k]; ok && time.Since(c.at) < latestTTL {
		m.mu.Unlock()
		return c.value
	}
	m.mu.Unlock()

	var v string
	if rel, err := fetchRelease(ctx, ytdlpRepo); err == nil {
		v = strings.TrimPrefix(rel.TagName, "v")
	}
	m.mu.Lock()
	m.latests[k] = cached{v, time.Now()}
	m.mu.Unlock()
	return v
}

const ytdlpRepo = "yt-dlp/yt-dlp"

// Status 汇总一个组件的当前状态。ctx 用于查上游版本,超时不影响本地信息。
func (m *Manager) Status(ctx context.Context, k Kind) Info {
	v := m.Version(k)
	info := Info{
		Kind: k, Installed: v != "", Version: v,
		Channel: m.Channel(k), Path: m.Path(k),
	}
	if !m.Managed(k) {
		return info
	}
	info.Latest = m.Latest(ctx, k)
	if info.Installed && info.Latest != "" && info.Latest != info.Version {
		info.Outdated = true
	}
	return info
}

// ---- 安装/升级 ----

// Install 下载并安装(或升级)一个组件。只有 yt-dlp 走这条路。
func (m *Manager) Install(ctx context.Context, k Kind) error {
	if k != Ytdlp {
		return errors.New("这个组件不由网页管理,升级方式见下方说明")
	}
	if !m.Managed(k) {
		return errors.New("当前以本机开发方式运行,请用系统包管理器安装 yt-dlp")
	}
	m.mu.Lock()
	if m.installing[k] {
		m.mu.Unlock()
		return errors.New("该组件正在下载中")
	}
	m.installing[k] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.installing, k)
		m.mu.Unlock()
	}()

	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	if err := m.installYtdlp(ctx); err != nil {
		return err
	}
	m.invalidate(k)
	return nil
}

func download(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("下载失败,服务器返回 %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// findAsset 在 release 里挑出符合当前架构的产物。
func findAsset(rel *ghRelease, candidates []string) (string, error) {
	for _, want := range candidates {
		for _, a := range rel.Assets {
			if a.Name == want {
				return a.URL, nil
			}
		}
	}
	return "", fmt.Errorf("上游没有适合本机架构(%s)的构建", runtime.GOARCH)
}

func (m *Manager) installYtdlp(ctx context.Context) error {
	// yt-dlp 官方提供 musl 独立二进制,下下来就能直接跑
	names := []string{"yt-dlp_musllinux", "yt-dlp_linux"}
	if runtime.GOARCH == "arm64" {
		names = []string{"yt-dlp_musllinux_aarch64", "yt-dlp_linux_aarch64"}
	}
	rel, err := fetchRelease(ctx, ytdlpRepo)
	if err != nil {
		return err
	}
	url, err := findAsset(rel, names)
	if err != nil {
		return err
	}
	body, err := download(ctx, url)
	if err != nil {
		return err
	}
	defer body.Close()
	return writeExecutable(m.Path(Ytdlp), body)
}
