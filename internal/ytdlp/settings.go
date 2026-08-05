package ytdlp

// yt下载的高级设置。存在的理由只有一个:YouTube 对机房 IP 越来越不
// 客气,常见表现是
//
//	ERROR: [youtube] xxx: Sign in to confirm you're not a bot.
//
// 官方 wiki 给的可行解法是带上浏览器导出的 cookies(次选是配代理、
// 或换一个 player client)。这三样都做成设置项,不让用户去碰命令行。
//
// cookies 落在**配置目录**(和 DB、头像同级),不在网盘里——所以既不
// 会出现在文件列表/WebDAV,也不会被整盘导出带走。

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"pocketdrive/internal/db"
	"pocketdrive/internal/httpx"
)

const (
	settingsKey = "ytdlp_settings"
	// 用户上传的原件。yt-dlp 每跑一次都会把刷新后的 cookie 写回
	// --cookies 指向的文件,所以真正传给它的是每次现拷的副本,
	// 原件保持用户上传时的样子(无痕窗口导出的 cookie 不该被轮换)。
	cookieFile    = "yt-cookies.txt"
	cookieRunFile = "yt-cookies.run.txt"
	maxCookieSize = 1 << 20
)

// playerClients 是 --extractor-args youtube:player_client= 的白名单。
// 用户只能选 key,任何时候都不会把用户输入拼进 yt-dlp 的参数里。
// 取值参考 yt-dlp wiki 的 PO Token Guide(不同客户端对 PO Token /
// 账号 cookie 的要求不一样,所以留给用户试)。
var playerClients = map[string]bool{
	"":             true, // 跟随 yt-dlp 默认
	"default":      true,
	"tv":           true,
	"tv_simply":    true,
	"web_safari":   true,
	"mweb":         true,
	"ios":          true,
	"android_vr":   true,
	"web_embedded": true,
}

// 这些客户端不支持账号 cookies。选中它们时 yt-dlp 即使收到了
// --cookies 也不能用里面的登录态通过 YouTube 验证。
var noAccountCookieClients = map[string]bool{
	"tv_simply":  true,
	"ios":        true,
	"android_vr": true,
}

var proxySchemes = map[string]bool{
	"http": true, "https": true, "socks4": true, "socks5": true, "socks5h": true,
}

type Settings struct {
	// Proxy 形如 socks5://127.0.0.1:1080,留空为直连
	Proxy string `json:"proxy"`
	// PlayerClient 留空表示不加 --extractor-args
	PlayerClient string `json:"playerClient"`
}

func (m *Manager) Settings() Settings {
	var s Settings
	var st db.Setting
	if m.db.First(&st, "key = ?", settingsKey).Error == nil && st.Value != "" {
		_ = json.Unmarshal([]byte(st.Value), &s)
	}
	return s
}

func (m *Manager) saveSettings(s Settings) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return m.db.Save(&db.Setting{Key: settingsKey, Value: string(b)}).Error
}

func validateSettings(s *Settings) error {
	s.Proxy = strings.TrimSpace(s.Proxy)
	if s.Proxy != "" {
		u, err := url.Parse(s.Proxy)
		if err != nil || u.Host == "" || !proxySchemes[u.Scheme] {
			return errors.New("代理格式不对,例:socks5://127.0.0.1:1080 或 http://127.0.0.1:8080")
		}
	}
	if !playerClients[s.PlayerClient] {
		return errors.New("未知的播放器客户端")
	}
	return nil
}

// ---- cookies ----

func (m *Manager) cookiePath() string {
	if m.confDir == "" {
		return ""
	}
	return filepath.Join(m.confDir, cookieFile)
}

type cookieStatus struct {
	Valid       bool   `json:"valid"`
	Message     string `json:"message"`
	CookieCount int    `json:"cookieCount"`
	AuthCount   int    `json:"authCount"`
}

func (m *Manager) cookieInfo() (bool, string, cookieStatus) {
	p := m.cookiePath()
	if p == "" {
		return false, "", cookieStatus{Message: "当前部署没有配置目录"}
	}
	fi, err := os.Stat(p)
	if err != nil || fi.Size() == 0 {
		return false, "", cookieStatus{Message: "未配置 cookies"}
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return true, fi.ModTime().Format(time.RFC3339), cookieStatus{Message: "cookies 文件读取失败"}
	}
	return true, fi.ModTime().Format(time.RFC3339), inspectCookies(string(b), time.Now())
}

// netscapeHeader 是 yt-dlp 认 cookie 文件的必要条件:少了这一行它会
// 直接报 "does not look like a Netscape format cookies file"。浏览器
// 插件导出的文件基本都带,少数会漏,这里补上而不是把用户挡回去。
const netscapeHeader = "# Netscape HTTP Cookie File"

var youtubeAuthCookies = map[string]bool{
	"SID": true, "HSID": true, "SSID": true, "APISID": true, "SAPISID": true,
	"__Secure-1PAPISID": true, "__Secure-3PAPISID": true, "LOGIN_INFO": true,
}

// inspectCookies 只统计域名、名称和过期时间，不保存也不返回 cookie 值。
func inspectCookies(raw string, now time.Time) cookieStatus {
	var st cookieStatus
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#HttpOnly_") {
			line = strings.TrimPrefix(line, "#HttpOnly_")
		} else if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 7 {
			continue
		}
		domain := strings.TrimPrefix(strings.ToLower(fields[0]), ".")
		if !strings.HasSuffix(domain, "youtube.com") && !strings.HasSuffix(domain, "google.com") {
			continue
		}
		st.CookieCount++
		expires, _ := strconv.ParseInt(fields[4], 10, 64)
		active := expires == 0 || expires > now.Unix()
		if active && youtubeAuthCookies[fields[5]] {
			st.AuthCount++
		}
	}
	switch {
	case st.CookieCount == 0:
		st.Message = "文件里没有 youtube.com/google.com 的 cookies"
	case st.AuthCount == 0:
		st.Message = "有 YouTube cookies，但没有未过期的登录凭据；请登录后重新导出"
	default:
		st.Valid = true
		st.Message = "检测到可用的 YouTube 登录凭据"
	}
	return st
}

// normalizeCookies 校验并补全 cookie 文本。判定标准取最宽松的一条:
// 至少有一行是 tab 分隔的 7 段。
func normalizeCookies(raw string) (string, error) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("内容是空的")
	}
	ok := false
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		if len(strings.Split(line, "\t")) >= 7 {
			ok = true
			break
		}
	}
	if !ok {
		return "", errors.New("这不像 Netscape 格式的 cookies.txt(每行应是 tab 分隔的 7 段)。" +
			"请用浏览器的 cookies.txt 导出插件另存,不要复制网页上的文字")
	}
	status := inspectCookies(raw, time.Now())
	if !status.Valid {
		return "", errors.New(status.Message)
	}
	if !strings.HasPrefix(raw, netscapeHeader) {
		raw = netscapeHeader + "\n" + raw
	}
	return raw + "\n", nil
}

// runCookies 把原件拷一份出来给这次任务用,返回副本路径;没有配 cookie
// 时返回空串。
func (m *Manager) runCookies() string {
	src := m.cookiePath()
	if src == "" {
		return ""
	}
	b, err := os.ReadFile(src)
	if err != nil || len(b) == 0 {
		return ""
	}
	dst := filepath.Join(m.confDir, cookieRunFile)
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		return ""
	}
	return dst
}

// ---- HTTP handlers ----

func (m *Manager) HandleGetSettings(w http.ResponseWriter, r *http.Request) {
	has, at, status := m.cookieInfo()
	httpx.JSON(w, http.StatusOK, map[string]any{
		"settings":         m.Settings(),
		"hasCookies":       has,
		"cookiesUpdated":   at,
		"cookiesSupported": m.confDir != "",
		"cookieStatus":     status,
	})
}

func (m *Manager) HandleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var s Settings
	if err := httpx.Decode(r, &s); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := validateSettings(&s); err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := m.saveSettings(s); err != nil {
		httpx.Err(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "settings": s})
}

// HandleSetCookies 接收 {content}:非空为保存,空串为删除。
func (m *Manager) HandleSetCookies(w http.ResponseWriter, r *http.Request) {
	if m.confDir == "" {
		httpx.Err(w, http.StatusBadRequest, "当前部署没有配置目录,无法保存 cookies")
		return
	}
	var req struct {
		Content string `json:"content"`
	}
	if err := httpx.DecodeN(r, &req, maxCookieSize+4096); err != nil {
		httpx.Err(w, http.StatusBadRequest, "请求格式错误(文件过大?)")
		return
	}
	p := m.cookiePath()
	if strings.TrimSpace(req.Content) == "" {
		_ = os.Remove(p)
		_ = os.Remove(filepath.Join(m.confDir, cookieRunFile))
		httpx.JSON(w, http.StatusOK, map[string]any{"ok": true, "hasCookies": false})
		return
	}
	if len(req.Content) > maxCookieSize {
		httpx.Err(w, http.StatusBadRequest, "cookies 文件过大")
		return
	}
	content, err := normalizeCookies(req.Content)
	if err != nil {
		httpx.Err(w, http.StatusBadRequest, err.Error())
		return
	}
	// 0600:里面是能直接登进 YouTube 账号的凭据
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		httpx.Err(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	has, at, status := m.cookieInfo()
	httpx.JSON(w, http.StatusOK, map[string]any{
		"ok": true, "hasCookies": has, "cookiesUpdated": at, "cookieStatus": status,
	})
}
