// Package components 管理 PocketDrive 依赖的三个外部程序:
// yt-dlp、aria2c、ffmpeg。
//
// 它们全部装在 /config/bin(volume 内)而不是镜像里,原因是镜像里的
// 文件一重启就回到出厂状态——用户在网页上点的「更新」会白点。装在
// volume 里之后,三个组件都能在网页里独立升级并一直保留。
//
// 首次启动时,如果 volume 里还没有,就从镜像内置的副本复制过去(见
// EnsureBundled);镜像没内置的则按需从上游下载。
//
// 本机开发(Windows/macOS)不下载任何东西:BinDir 为空时一律回退到
// PATH 里的同名程序。
package components

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ulikunitz/xz"
)

// Kind 是组件标识,同时也是 /config/bin 下的文件名。
type Kind string

const (
	Ytdlp  Kind = "yt-dlp"
	Aria2  Kind = "aria2c"
	FFmpeg Kind = "ffmpeg"
)

const (
	downloadTimeout = 10 * time.Minute
	maxDownload     = 200 << 20 // 单个组件最大 200MB,防止下到意外的大文件
	versionTTL      = 10 * time.Minute
	latestTTL       = time.Hour // GitHub 未认证接口每小时 60 次,查勤了会被限流
)

// Info 是一个组件对外呈现的状态。
type Info struct {
	Kind      Kind   `json:"kind"`
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Latest    string `json:"latest"`   // 上游最新版;查不到时为空
	Outdated  bool   `json:"outdated"` // 能比较且确实旧了
	Managed   bool   `json:"managed"`  // 装在 volume 里、可在网页升级
	Path      string `json:"path"`
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

// Path 返回组件的可执行文件路径。托管模式下即 /config/bin/<name>,
// 否则回退到裸名字(由 PATH 解析)。
func (m *Manager) Path(k Kind) string {
	if m.binDir == "" {
		return exeName(string(k))
	}
	return filepath.Join(m.binDir, exeName(string(k)))
}

// Managed 报告组件是否由本程序托管(即能不能在网页里升级)。
func (m *Manager) Managed() bool { return m.binDir != "" }

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
func (m *Manager) Version(k Kind) string {
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
	return "-version" // aria2c 与 ffmpeg 都认 -version
}

// parseVersion 从各家五花八门的输出里抠出版本号。
func parseVersion(k Kind, out string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	line = strings.TrimSpace(line)
	switch k {
	case Ytdlp:
		return line // yt-dlp 直接打印 "2026.01.01"
	case Aria2:
		// "aria2 version 1.37.0"
		if i := strings.Index(line, "version "); i >= 0 {
			return strings.TrimSpace(line[i+len("version "):])
		}
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

// Latest 查上游最新版本号(带缓存)。ffmpeg 的构建仓库用滚动 tag,
// 没有可比较的版本号,返回空串——它只能「重新装一次最新的」。
func (m *Manager) Latest(ctx context.Context, k Kind) string {
	if k == FFmpeg {
		return ""
	}
	m.mu.Lock()
	if c, ok := m.latests[k]; ok && time.Since(c.at) < latestTTL {
		m.mu.Unlock()
		return c.value
	}
	m.mu.Unlock()

	var v string
	if rel, err := fetchRelease(ctx, repoOf(k)); err == nil {
		v = strings.TrimPrefix(rel.TagName, "v")
	}
	m.mu.Lock()
	m.latests[k] = cached{v, time.Now()}
	m.mu.Unlock()
	return v
}

func repoOf(k Kind) string {
	switch k {
	case Ytdlp:
		return "yt-dlp/yt-dlp"
	case Aria2:
		return "abcfy2/aria2-static-build"
	case FFmpeg:
		return "BtbN/FFmpeg-Builds"
	}
	return ""
}

// Status 汇总一个组件的当前状态。ctx 用于查上游版本,超时不影响本地信息。
func (m *Manager) Status(ctx context.Context, k Kind) Info {
	v := m.Version(k)
	info := Info{
		Kind: k, Installed: v != "", Version: v,
		Managed: m.Managed(), Path: m.Path(k),
	}
	if !m.Managed() {
		return info // 本机开发:装没装、什么版本,都由用户自己的 PATH 决定
	}
	info.Latest = m.Latest(ctx, k)
	if info.Installed && info.Latest != "" && info.Latest != info.Version {
		info.Outdated = true
	}
	return info
}

// ---- 安装/升级 ----

// Install 下载并安装(或升级)一个组件。
func (m *Manager) Install(ctx context.Context, k Kind) error {
	if !m.Managed() {
		return errors.New("当前部署方式不托管组件,请用系统包管理器安装")
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

	var err error
	switch k {
	case Ytdlp:
		err = m.installYtdlp(ctx)
	case Aria2:
		err = m.installAria2(ctx)
	case FFmpeg:
		err = m.installFFmpeg(ctx)
	default:
		err = errors.New("未知组件")
	}
	if err != nil {
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
	rel, err := fetchRelease(ctx, repoOf(Ytdlp))
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

func (m *Manager) installAria2(ctx context.Context) error {
	// 静态构建打在 zip 里,zip 的中央目录在末尾,得整包读进内存才能定位
	names := []string{"aria2-x86_64-linux-musl_static.zip"}
	if runtime.GOARCH == "arm64" {
		names = []string{"aria2-aarch64-linux-musl_static.zip"}
	}
	rel, err := fetchRelease(ctx, repoOf(Aria2))
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
	buf, err := io.ReadAll(io.LimitReader(body, maxDownload))
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(bytes.NewReader(buf), int64(len(buf)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		if path.Base(f.Name) != "aria2c" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		return writeExecutable(m.Path(Aria2), rc)
	}
	return errors.New("下载的压缩包里没有 aria2c")
}

func (m *Manager) installFFmpeg(ctx context.Context) error {
	// BtbN 的构建是 tar.xz,二进制在 <顶层目录>/bin/ffmpeg
	names := []string{"ffmpeg-master-latest-linux64-gpl.tar.xz"}
	if runtime.GOARCH == "arm64" {
		names = []string{"ffmpeg-master-latest-linuxarm64-gpl.tar.xz"}
	}
	rel, err := fetchRelease(ctx, repoOf(FFmpeg))
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

	xr, err := xz.NewReader(io.LimitReader(body, maxDownload))
	if err != nil {
		return err
	}
	tr := tar.NewReader(xr)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		// 包里还有 ffprobe/ffplay 和一堆文档,只要 bin/ffmpeg
		if hdr.Typeflag == tar.TypeReg &&
			path.Base(hdr.Name) == "ffmpeg" && strings.Contains(hdr.Name, "/bin/") {
			return writeExecutable(m.Path(FFmpeg), tr)
		}
	}
	return errors.New("下载的压缩包里没有 ffmpeg")
}
