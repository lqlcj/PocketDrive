# PocketDrive — 口袋网盘

自托管个人网盘,专为小内存 VPS(1-2G)设计。前端 React 19 + Tailwind CSS v4 +
shadcn 式自研组件 + lucide 图标,后端 Go 单二进制,SQLite 存储元数据。

## 功能

- **文件管理(即主页)**:目录树导航 + 上传(拖拽,>64MB 自动分片断点重传)/ 下载 /
  重命名 / 移动(目录选择器或拖拽到文件夹)/ 新建文件夹 / 文件按上传时间倒序
- **两种视图**:列表模式 + 缩略图模式(图片缩略图、视频封面,一键切换)
- **文件夹图标**:默认 lucide 图标,也可给文件夹挑个 emoji,目录树和列表同步显示
- **回收站**:删除先进垃圾桶,可还原或永久删除,30 天自动清理
- **全局搜索**:顶栏搜索全部文件名(内存索引,60s 自动刷新)
- **在线预览**:Markdown、图片(可翻页)、视频、音频、文本/代码、PDF,以及
  **Word(.docx)/ Excel(.xlsx/.xls)/ PPT(.pptx)** —— Office 预览全部在
  浏览器端渲染并按需加载,服务器只出文件流,1G 小鸡也毫无压力
- **Markdown 笔记**:左编辑右实时预览,Ctrl+S 保存
- **分享**:分享页 `/s/xxx`(可设提取密码+过期时间)或直链 `/d/xxx`(给播放器/下载工具),
  「分享管理」页统一查看删除
- **WebDAV**:`/dav/` 暴露整个网盘,手机播放器/文件管理器直连(与网页同端口)
- **离线下载**:aria2 承载,http(s)/ftp 直链、磁力,以及**上传 .torrent 种子文件**;
  「下载设置」页网页化管理(并发数/上下行限速即时生效、做种策略、BT tracker 每日自动更新、默认目录)
- **yt下载**:yt-dlp 网页端;画质预设(最佳/1080p/720p/480p)、仅音频(m4a/mp3)、
  嵌入封面/元数据、中英字幕、**播放列表批量下载**(存进播放列表名子文件夹,文件名带序号)
- **动画登录页**:一群会盯着你鼠标看的小家伙,输密码时集体扭头回避
  (移植自 [animatedlogin-react](https://github.com/Keduoli03/animatedlogin-react),MIT)
- **黑夜模式**:暖沙 / 暖炭双主题(Claude 风配色),切换带过渡动画
- **个性化**:emoji 头像(一排动物一排植物)、可改用户名(WebDAV 同步生效)
- 移动端响应式,手机浏览器可用

视频封面缩略图需要 ffmpeg(Docker 镜像已内置;本机没有则回退为图标)。

整个网盘就是一个目录(`/data`):网页、WebDAV、aria2、yt-dlp 全部读写同一目录,
文件夹结构完全由你自己组织。回收站是数据目录下的隐藏目录 `.trash`(WebDAV 中可见,勿手动动它)。

## 部署

### 方式一:VPS 一键安装(推荐)

```bash
curl -fsSL https://raw.githubusercontent.com/lqlcj/PocketDrive/main/scripts/install.sh | sudo bash
```

脚本会自动:装 docker(如果没有)→ 建 `/opt/pocketdrive` → 生成随机密码 →
拉取官方镜像并启动 pocketdrive + aria2。装完直接打印访问地址和密码。
重跑同一条命令即为升级(数据、密码不动)。

### 方式二:1Panel 编排安装

1. 1Panel 后台 → **容器** → **编排** → **创建编排**
2. 名称随意(如 `pocketdrive`),把下面的内容贴进去:

```yaml
services:
    pocketdrive:
        image: ghcr.io/lqlcj/pocketdrive:latest
        container_name: pocketdrive
        restart: unless-stopped
        ports:
            - '16688:16688'
        environment:
            - POCKETDRIVE_DATA_DIR=/data
            - POCKETDRIVE_DB=/config/pocketdrive.db
            - POCKETDRIVE_ADMIN_USER=admin
            - POCKETDRIVE_ADMIN_PASSWORD=改成你的登录密码
            - POCKETDRIVE_ARIA2_RPC=http://aria2:6800/jsonrpc
            - POCKETDRIVE_ARIA2_SECRET=改成你的rpc密钥
            - POCKETDRIVE_ARIA2_DATA_DIR=/data
        volumes:
            - ./data:/data
            - ./config:/config
        depends_on:
            - aria2

    aria2:
        image: p3terx/aria2-pro
        container_name: pocketdrive-aria2
        restart: unless-stopped
        environment:
            - RPC_SECRET=改成你的rpc密钥
            - LISTEN_PORT=6888
            - MAX_CONCURRENT_DOWNLOADS=3
        volumes:
            - ./data:/data
        ports:
            - '6888:6888'
            - '6888:6888/udp'
```

3. 改掉两处密码 → **确认**。之后在 1Panel 的防火墙页放行 `16688`(以及可选的 `6888`)。
4. 升级:编排详情页对 pocketdrive 服务「拉取镜像并重建」即可,数据在编排目录的 `data/` 里。

### 方式三:git clone 后 compose 构建

```bash
git clone https://github.com/lqlcj/PocketDrive && cd PocketDrive/docker
cat > .env <<'EOF'
ARIA2_SECRET=换成你的rpc密钥
POCKETDRIVE_ADMIN_PASSWORD=换成你的登录密码
EOF
# 默认拉官方镜像;想本地构建就按 docker-compose.yml 顶部注释切换 build 模式
docker compose up -d
```

访问 `http://VPS_IP:16688`,用户名默认 `admin`。
`POCKETDRIVE_ADMIN_PASSWORD` 留空则首次启动随机生成并打印在 `docker logs pocketdrive` 里。

WebDAV 地址:`http://VPS_IP:16688/dav/`,账号密码与网页登录相同。

## 端口说明(常见疑问)

| 端口 | 用途 | 必须开放? |
|---|---|---|
| `16688/tcp` | 网页 + API + WebDAV + 分享链接 | ✅ 是 |
| `6888/tcp+udp` | aria2 的 BT 监听端口(接受其他 peer 主动连接、DHT) | 可选:不开也能下载,但冷门种子连接数少、速度慢 |
| `6800` | aria2 RPC(容器间内部通信) | ❌ 不要对公网开放 |

- **网页和 WebDAV 同端口没问题**:WebDAV 只是同一个 HTTP 服务下的 `/dav/` 路径,
  底层同样是标准 HTTP(ServeContent:流式 + Range + 条件请求)。Cloudreve 等项目
  拆端口通常是因为 WebDAV 由独立进程承载或想单独做 TLS/鉴权,并非协议要求。
- **离线下载(http/磁力/种子)全部是出站连接**,不需要额外开端口;只有 BT 想要
  更好的连通性时才放行 6888。
- **yt-dlp 纯出站**,无需任何端口。

### 资源占用(2G VPS 实测预期)

| 组件 | 常驻内存 |
|---|---|
| PocketDrive(Go,含前端) | ~40-80MB |
| aria2 | ~30-100MB |
| yt-dlp(仅下载时) | 100-300MB 瞬时 |

上传下载全程流式,视频/音频播放走 HTTP Range,不占内存;Office/PDF 预览在浏览器端渲染,
服务器零额外开销。
**视频只直连播放浏览器支持的格式**(mp4/webm 等);mkv/rmvb 等请下载后本地播放
(小内存装不下实时转码,这是刻意取舍)。

## 配置(环境变量)

| 变量 | 默认 | 说明 |
|---|---|---|
| `POCKETDRIVE_ADDR` | `:16688` | 监听地址 |
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
# 终端 1:后端(:16688)
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
- 登录失败限流:同 IP 连错 5 次封 5 分钟;WebDAV Basic Auth,bcrypt + 成功凭据缓存
- 文件操作全部经 `os.Root`(Go 1.25+)防路径穿越/symlink 逃逸
- aria2/yt-dlp 均 exec 直调不经 shell;URL 按协议白名单校验;yt-dlp 参数只来自固定预设模板;
  上传的 .torrent 做 bencode 头校验 + 16MB 上限
- Office 预览为纯前端渲染(docx-preview / SheetJS / pptx-preview 动态加载),后端不解析文档

## 已知限制(后续候选)

- 不支持子路径部署(须挂在域名根)
- 分享仅支持单个文件(文件夹分享未做);无多用户、无磁盘配额
- 全局搜索只搜文件名;文档内容全文检索(SQLite FTS5)是后续候选
- 旧版二进制 Office 格式(.doc/.ppt)不支持预览(.xls 可以),请下载后本地打开
- 在线解压/压缩、整盘打包导出/导入(VPS 迁移)规划中(下一轮「档案功能」)
- WebDAV 尚未在真实手机播放器上实测
