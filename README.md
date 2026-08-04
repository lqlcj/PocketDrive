# PocketDrive — 口袋网盘

自托管个人网盘,专为小内存 VPS(1-2G)设计。前端 React 19 + Tailwind CSS v4 +
shadcn 式自研组件 + lucide 图标,后端 Go 单二进制,SQLite 存储元数据。

## 功能

- **文件管理(即主页)**:目录树导航 + 上传文件/整个文件夹(拖拽,>64MB 自动分片)/
  下载 / 重命名 / 移动(目录选择器或拖拽到文件夹)/ 新建文件夹 / 文件按上传时间倒序 / 分页
- **上传管理器**:右下角常驻面板,多文件排队上传,单个文件可暂停、继续、取消,
  可收成一条。切换页面不打断上传
- **断点续传**:大文件传到一半断网、刷新页面、甚至服务端重启,重新选中同一个文件
  都会接着上次的进度传;下载侧支持 Range,播放器可以随意拖进度
- **外部存储**:挂载 Cloudflare R2 / AWS S3 / MinIO 等 S3 兼容对象存储,显示为根目录下的
  `@名称` 文件夹,网页与 WebDAV 通用。可为每个挂载单独设置容量上限。上传经服务端中转、
  下载 302 到预签名地址,**桶上不需要配置任何 CORS**,私有桶即可
- **组件自管**:aria2 / yt-dlp / ffmpeg 都装在 config 卷里,在设置页各自一键升级,
  有新版会标出来,升级结果跨容器重启保留
- **在线压缩/解压**:选中文件或文件夹压成 zip / tar.gz;zip、tar.gz、tar.xz、tar 可在线解压
- **整盘导出/导入**:一个 tar.gz 打包网盘文件 + 配置库(分享链接、下载历史、
  文件夹图标、存储策略),换 VPS 时导出再导入即可
- **两种视图**:列表模式 + 缩略图模式(图片缩略图、视频封面,一键切换)
- **文件夹图标**:默认 lucide 图标,也可给文件夹挑个 emoji,目录树和列表同步显示
- **回收站**:删除先进垃圾桶,可还原或永久删除,30 天自动清理
- **全局搜索**:顶栏搜索全部文件名(内存索引,60s 自动刷新)
- **在线预览**:Markdown、图片(可翻页)、视频、音频、文本/代码、PDF,以及
  **Word(.docx)/ Excel(.xlsx/.xls)/ PPT(.pptx)** —— Office 预览全部在
  浏览器端渲染并按需加载,服务器只出文件流,1G 小鸡也毫无压力
- **Markdown 笔记**:左编辑右实时预览,Ctrl+S 保存
- **分享**:分享页 `/s/xxx`(可设提取密码+过期时间)或直链 `/d/xxx/文件名.mp4`
  (URL 自带真实文件名后缀,播放器/下载工具按后缀识别;不带文件名段的旧链接同样有效),
  「分享管理」页统一查看删除
- **WebDAV**:`/dav/` 暴露整个网盘,手机播放器/文件管理器直连(与网页同端口)
- **离线下载**:aria2 承载,http(s)/ftp 直链、磁力,以及**上传 .torrent 种子文件**;
  「下载设置」页网页化管理(并发数/上下行限速即时生效、做种策略、BT tracker 每日自动更新、默认目录)
- **yt下载**:yt-dlp 网页端;画质预设(最佳/1080p/720p/480p)、仅音频(m4a/mp3)、
  嵌入封面/元数据、中英字幕、**播放列表批量下载**(存进播放列表名子文件夹,文件名带序号)
- **动画登录页**:一群会盯着你鼠标看的小家伙,输密码时集体扭头回避
  (移植自 [animatedlogin-react](https://github.com/Keduoli03/animatedlogin-react),MIT)
- **黑夜模式**:暖沙 / 暖炭双主题(Claude 风配色),切换带过渡动画
- **个性化**:自定义头像(上传图片,自动裁成正方形;不上传就用用户名首字母)、
  可改用户名(WebDAV 同步生效)。头像存在配置目录,不会出现在网盘和 WebDAV 里
- 移动端响应式,手机浏览器可用

视频封面缩略图需要 ffmpeg(Docker 镜像已内置,也能在设置页里升级;本机没有则回退为图标)。

整个网盘就是一个目录(`/data`):网页、WebDAV、aria2、yt-dlp 全部读写同一目录,
文件夹结构完全由你自己组织。回收站是数据目录下的隐藏目录 `.trash`(WebDAV 中可见,勿手动动它)。

## 部署

### 方式一:VPS 一键安装(推荐)

```bash
curl -fsSL https://raw.githubusercontent.com/lqlcj/PocketDrive/main/scripts/install.sh | sudo bash
```

脚本会自动:装 docker(如果没有)→ 建 `/opt/pocketdrive` → 生成随机密码 →
拉取官方镜像并启动(单个容器,aria2 已内置)。装完直接打印访问地址和密码。
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
            # aria2 的 BT 监听端口(可选,不开也能下载)
            - '6888:6888'
            - '6888:6888/udp'
        environment:
            - POCKETDRIVE_DATA_DIR=/data
            - POCKETDRIVE_DB=/config/pocketdrive.db
            - POCKETDRIVE_ADMIN_USER=admin
            - POCKETDRIVE_ADMIN_PASSWORD=改成你的登录密码
        volumes:
            - ./data:/data
            - ./config:/config
```

3. 改掉密码 → **确认**。之后在 1Panel 的防火墙页放行 `16688`(以及可选的 `6888`)。
4. 升级:编排详情页对 pocketdrive 服务「拉取镜像并重建」即可,数据在编排目录的 `data/` 里。
   aria2 / yt-dlp / ffmpeg 三个组件不随镜像走,在网页设置页里各自升级即可。

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

## 升级与卸载

### 升级

```bash
cd /opt/pocketdrive          # 一键安装的默认目录
docker compose pull          # 拉新镜像
docker compose up -d
```

一键安装的用户重跑安装脚本效果相同,数据和密码不受影响。

**aria2 / yt-dlp / ffmpeg 不随镜像走** —— 它们装在 `config` 卷里,在
**网页 → 设置 → 组件状态** 里各自点一下就能升级,升级结果跨容器重启保留,
不需要在服务器上敲任何命令。有新版本时那里会直接标出来。

这也意味着换 PocketDrive 镜像不会把你升级过的组件退回旧版。

### 卸载

```bash
cd /opt/pocketdrive

# 1. 只停服务,数据全部保留(之后 docker compose up -d 就能回来)
docker compose down

# 2. 连同容器卷一起删,再删掉整个目录 —— 网盘文件和配置库都会没
docker compose down -v
cd / && rm -rf /opt/pocketdrive

# 3. 顺手清掉镜像(可选)
docker rmi ghcr.io/lqlcj/pocketdrive:latest
```

旧版(pocketdrive + aria2 两个容器)如果还留着 aria2 容器,一并删掉:
`docker rm -f pocketdrive-aria2 && docker rmi p3terx/aria2-pro`

删之前先在 **设置 → 备份与迁移 → 导出整盘备份** 下载一份,里面有网盘文件和
配置库(含分享链接、下载历史、存储策略密钥),换机器时直接导入就能恢复。

数据放在哪:

| 路径 | 内容 |
|---|---|
| `/opt/pocketdrive/data` | 网盘文件本体(WebDAV、aria2、yt-dlp 都读写这里) |
| `/opt/pocketdrive/config` | SQLite 配置库、缩略图缓存、分片上传暂存、yt-dlp 二进制 |

挂载的外部存储(R2/S3)里的文件不在上面两个目录里,卸载 PocketDrive 不会动它们。

## 端口说明(常见疑问)

| 端口 | 用途 | 必须开放? |
|---|---|---|
| `16688/tcp` | 网页 + API + WebDAV + 分享链接 | ✅ 是 |
| `6888/tcp+udp` | aria2 的 BT 监听端口(接受其他 peer 主动连接、DHT) | 可选:不开也能下载,但冷门种子连接数少、速度慢 |
| `6800` | aria2 RPC,只监听容器内的回环地址 | ❌ 不对外暴露,也无需映射 |

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
| `POCKETDRIVE_ARIA2_RPC` | 空 | 留空 = 在本容器内启动 aria2 子进程(推荐)。填了则连外部 aria2,不再启动内置的 |
| `POCKETDRIVE_ARIA2_SECRET` | 自动生成 | aria2 RPC 密钥。内置模式下自动生成并存库 |
| `POCKETDRIVE_ARIA2_DATA_DIR` | 同 DATA_DIR | aria2 进程视角的数据目录路径 |
| `POCKETDRIVE_ARIA2_BT_PORT` | `6888` | aria2 的 BT/DHT 监听端口 |
| `POCKETDRIVE_BIN_DIR` | 空 | 组件(aria2c / yt-dlp / ffmpeg)的安装目录,镜像里是 `/config/bin`。留空 = 不托管,用 PATH 里的版本 |
| `POCKETDRIVE_BIN_BUNDLED` | 空 | 镜像内置的组件副本目录,首次启动时复制进上面那个目录 |

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
- 全局搜索只搜文件名,且不含外部存储;文档内容全文检索(SQLite FTS5)是后续候选
- 旧版二进制 Office 格式(.doc/.ppt)不支持预览(.xls 可以),请下载后本地打开
- 压缩只支持 zip / tar.gz;rar、7z 连解压也不支持(需要额外的二进制)
- 外部存储与本机存储的差异:删除不经回收站、不生成缩略图、不参与全局搜索、
  离线下载/yt下载仍落本机、不支持跨存储移动(下载后重新上传即可)
- WebDAV 尚未在真实手机播放器上实测
