# 🏝️ PocketDrive — 动森风格个人网盘

自托管个人网盘,专为小内存 VPS(2G 起)设计。前端 React 19 + Tailwind CSS v4 +
自研"岛屿手账"风组件(shadcn 式底层 + 动森皮肤,Radix 无头组件),后端 Go 单二进制,
SQLite 存储元数据。

## 功能

- **小岛主页**:小屋(文件)/ 储藏室(下载)/ 展览馆(照片)/ 留声机(音乐)/
  影院(视频)/ 笔记本,逛自己的数字岛
- **文件管理**:上传(拖拽)/ 下载 / 重命名 / 移动(目录选择器或拖拽到文件夹)/ 新建文件夹
- **两种视图**:列表模式 + 缩略图模式(图片缩略图、视频封面,一键切换)
- **回收站**:删除先进垃圾桶,可还原或永久删除,30 天自动清理
- **全局搜索**:顶栏搜索全岛文件名(内存索引,60s 自动刷新)
- **在线预览**:Markdown、图片(可翻页)、视频、音频、文本/代码
- **Markdown 笔记**:左编辑右实时预览,Ctrl+S 保存,笔记本页汇总全部 .md
- **分享**:分享页 `/s/xxx`(可设提取密码+过期时间)或直链 `/d/xxx`(给播放器/下载工具),
  设置页统一管理删除
- **WebDAV**:`/dav/` 暴露整个网盘,手机播放器/文件管理器直连任意子文件夹
- **离线下载**:aria2 承载,支持 http(s)/ftp 直链和磁力/BT,可视化选择保存目录
- **yt下载**:yt-dlp 网页端;画质预设(最佳/1080p/720p/480p)、仅音频(m4a/mp3)、
  嵌入封面/元数据、中英字幕
- **黑夜模式**:白天沙滩 / 夜晚海面双主题,一键切换
- **个性化**:emoji 头像、可改用户名(WebDAV 同步生效)
- 移动端响应式,手机浏览器可用

视频封面缩略图需要 ffmpeg(Docker 镜像已内置;本机没有则回退为图标)。

整个网盘就是一个目录(`/data`):网页、WebDAV、aria2、yt-dlp 全部读写同一目录,
文件夹结构完全由你自己组织。回收站是数据目录下的隐藏目录 `.trash`(WebDAV 中可见,勿手动动它)。

## 部署(Docker)

```bash
git clone <本仓库> && cd PocketDrive/docker
# 编辑 docker-compose.yml 顶部注释里说明的两个密码变量,或写 .env:
cat > .env <<'EOF'
ARIA2_SECRET=换成你的rpc密钥
POCKETDRIVE_ADMIN_PASSWORD=换成你的登录密码
EOF
docker compose up -d --build
```

访问 `http://VPS_IP:8080`,用户名默认 `admin`。
`POCKETDRIVE_ADMIN_PASSWORD` 留空则首次启动随机生成并打印在 `docker logs pocketdrive` 里。

WebDAV 地址:`http://VPS_IP:8080/dav/`,账号密码与网页登录相同。

### 资源占用(2G VPS 实测预期)

| 组件 | 常驻内存 |
|---|---|
| PocketDrive(Go,含前端) | ~40-80MB |
| aria2 | ~30-100MB |
| yt-dlp(仅下载时) | 100-300MB 瞬时 |

上传下载全程流式,视频/音频播放走 HTTP Range,不占内存。
**视频只直连播放浏览器支持的格式**(mp4/webm 等);mkv/rmvb 等请下载后本地播放
(2G 内存装不下实时转码,这是刻意取舍)。

## 配置(环境变量)

| 变量 | 默认 | 说明 |
|---|---|---|
| `POCKETDRIVE_ADDR` | `:8080` | 监听地址 |
| `POCKETDRIVE_DATA_DIR` | `./data` | 网盘根目录 |
| `POCKETDRIVE_DB` | `./pocketdrive.db` | SQLite 路径(放数据目录外,避免出现在网盘里) |
| `POCKETDRIVE_ADMIN_USER` | `admin` | 管理员用户名 |
| `POCKETDRIVE_ADMIN_PASSWORD` | 随机生成 | 初始密码,之后在设置页修改(存库) |
| `POCKETDRIVE_ARIA2_RPC` | `http://127.0.0.1:6800/jsonrpc` | aria2 RPC 地址 |
| `POCKETDRIVE_ARIA2_SECRET` | 空 | aria2 RPC 密钥 |
| `POCKETDRIVE_ARIA2_DATA_DIR` | 同 DATA_DIR | aria2 进程视角的数据目录路径 |
| `POCKETDRIVE_YTDLP` | `yt-dlp` | yt-dlp 可执行文件路径 |

## 本机开发(Windows)

```powershell
# 终端 1:后端(:8080)
./scripts/dev.ps1
# 终端 2:前端(:5173,代理 /api 与 /dav 到后端)
cd web; npm install; npm run dev
```

发版构建:`cd web && npm run build`,然后 `go build ./cmd/pocketdrive`
(前端产物经 go:embed 打进二进制)。

浏览器冒烟:后端跑起来后 `cd web && npm i --no-save playwright-core && node smoke.mjs`
(使用本机 Chrome,截图在 `web/shots/`)。

## 安全设计

- JWT 放 httpOnly cookie(SameSite=Strict),CSRF 校验 Origin/Sec-Fetch-Site
- WebDAV Basic Auth,bcrypt + 成功凭据缓存,失败限流(5 次错 5 分钟)
- 文件操作全部经 `os.Root`(Go 1.25+)防路径穿越/symlink 逃逸
- aria2/yt-dlp 均 exec 直调不经 shell;URL 按协议白名单校验;yt-dlp 参数只来自固定预设模板

## 已知限制(后续候选)

- 不支持子路径部署(须挂在域名根)
- 分享仅支持单个文件(文件夹分享未做);无多用户、无磁盘配额
- 全局搜索只搜文件名;文档内容全文检索(SQLite FTS5)是后续候选
- 离线下载不支持上传 .torrent 文件(可用磁力代替)
- yt下载不支持播放列表批量下载(刻意 `--no-playlist`)
- 小岛主页板块暂不支持自定义贴图(规划中)
- WebDAV 尚未在真实手机播放器上实测
