# PocketDrive — 口袋网盘

<img width="1983" height="793" alt="hengfu" src="https://github.com/user-attachments/assets/84fd00e9-185f-47ae-a4d6-99fec2c3c124" />

自托管个人网盘,专为小内存 VPS设计。前端 React,后端 Go ,SQLite 存储。

---

你的 512MB、1GB 内存的小鸡，是不是只够装个探针然后吃灰？送的存储不用又浪费？

市面上优秀网盘很多，可惜大多太重了——要装数据库、要装缓存、还带一堆插件，咱们小鸡根本扛不住。于是就有了这款轻量网盘。

虽然轻量，但功能不减。存储采用 SQLite，支持 WebDAV，内置 aria2 离线下载，还能自动更新 Tracker。文件预览直接调用浏览器能力，不堆插件。

也不打算做多用户和多客户端，一切目标都是轻量，让小鸡也能用，不能让它只配装探针。响应式已做好，移动端添加到桌面用 PWA 体验也很好。

## 资源占用情况

| 组件 | 常驻内存 |
|---|---|
| PocketDrive(Go,含前端) | ~40-80MB |
| aria2 | ~30-100MB |

上传下载全程流式,视频/音频播放走 HTTP Range,不占内存;Office/PDF 预览在浏览器端渲染,
服务器零额外开销。
**视频只直连播放浏览器支持的格式**(mp4/webm 等);mkv/rmvb 等请下载后本地播放
(小内存装不下实时转码,这是刻意取舍)。

## 功能

| 功能 |  |  |
|---|---|---|
| 文件管理 | WebDAV | 离线下载 |
| 断点续传 | 在线压缩/解压 | 整盘导出/导入 |
| 全局搜索 | 在线预览 | Markdown 笔记 |
| 分享 | 黑夜模式 | 移动端响应式 |



### 方式一:VPS 一键安装(推荐)

```bash
curl -fsSL https://raw.githubusercontent.com/lqlcj/PocketDrive/main/scripts/install.sh | sudo bash
```

脚本会自动:装 docker(如果没有)→ 建 `/opt/pocketdrive` → 生成随机密码和 aria2 RPC 密钥 →
拉起 pocketdrive 和 aria2 容器。装完直接打印访问地址和密码。
重跑同一条命令即为升级(数据、密码不动)。

开箱即用，`http://IP:16688` 就能访问

### 方式二:编排安装

以1panel为例:

1. 1Panel 后台 → **容器** → **编排** → **创建编排**
2. 名称随意(如 `pocketdrive`),把下面的内容贴进去:

```yaml
services:
    pocketdrive:
        image: ghcr.io/lqlcj/pocketdrive:latest
        container_name: pocketdrive
        restart: unless-stopped
        init: true
        ports:
            - '16688:16688'
        environment:
            - POCKETDRIVE_DATA_DIR=/data
            - POCKETDRIVE_DB=/config/pocketdrive.db
            - POCKETDRIVE_ADMIN_USER=改成你的用户名
            - POCKETDRIVE_ADMIN_PASSWORD=改成你的登录密码
            - POCKETDRIVE_ARIA2_RPC=http://aria2:6800/jsonrpc
            - POCKETDRIVE_ARIA2_SECRET=请填一段随机的 RPC 密钥
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
            - RPC_SECRET=请填一段随机的 RPC 密钥跟上面相同
            # BT 监听端口(可选,不开也能下载)
            - LISTEN_PORT=6888
            - MAX_CONCURRENT_DOWNLOADS=3
            # 这三行不能省:镜像默认让 aria2c 以 nobody(65534)运行,
            # 写不进 PocketDrive 以 root 建的目录,表现为下载任务报
            # "Permission denied" 或 "Download aborted."
            - PUID=0
            - PGID=0
            - UMASK_SET=022
        volumes:
            - ./data:/data
            # 挂出来,容器重启后没下完的任务还能接着下
            - ./config/aria2:/config
        ports:
            - '6888:6888'
            - '6888:6888/udp'

```

3. 修改管理员用户名和登录密码，并把 `POCKETDRIVE_ARIA2_SECRET` 填成随机密钥；
   PocketDrive 和 aria2 两处必须使用同一个值 → **确认**。
   之后在防火墙页放行 `16688`(以及可选的 `6888`)。
4. **以后升级请在 1Panel 编排详情页点击「拉取镜像并重建」,数据在编排目录的 `data/` 里**。
5. 开箱即用，`http://IP:16688` 就能访问

### 如果使用 Nginx/Caddy 等反向代理，编排内字段应改为：

``````
        ports:
            - '16688:16688'
            要改成:
        ports:
           - "127.0.0.1:16688:16688"
``````



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

统一使用手动拉取并重建编排:

```bash
cd /opt/pocketdrive          # 一键安装的默认目录
docker compose pull          # 拉新镜像
docker compose up -d
```

一键安装的用户重跑安装脚本效果相同,数据和密码不受影响。

这会同时更新 PocketDrive、ffmpeg 和 aria2,不会删除 `data/`、`config/` 中的数据。

### 卸载

```bash
cd /opt/pocketdrive

# 1. 只停服务,数据全部保留(之后 docker compose up -d 就能回来)
docker compose down

# 2. 连同容器卷一起删,再删掉整个目录 —— 网盘文件和配置库都会没
docker compose down -v
cd / && rm -rf /opt/pocketdrive

# 3. 顺手清掉镜像(可选)
docker rmi ghcr.io/lqlcj/pocketdrive:latest p3terx/aria2-pro
```

删之前先在 **设置 → 备份与迁移 → 导出整盘备份** 下载一份,里面有网盘文件和
配置库(含分享链接、下载历史、存储策略密钥),换机器时直接导入就能恢复。

数据放在哪:

| 路径 | 内容 |
|---|---|
| `/opt/pocketdrive/data` | 网盘文件本体(WebDAV、aria2 都读写这里) |
| `/opt/pocketdrive/config` | SQLite 配置库、缩略图缓存、分片上传暂存 |

挂载的外部存储(R2/S3)里的文件不在上面两个目录里,卸载 PocketDrive 不会动它们。

## 端口说明(常见疑问)

| 端口 | 用途 | 必须开放? |
|---|---|---|
| `16688/tcp` | 网页 + API + WebDAV + 分享链接 | ✅ 是 |
| `6888/tcp+udp` | aria2 的 BT 监听端口(接受其他 peer 主动连接、DHT) | 可选:不开也能下载,但冷门种子连接数少、速度慢 |
| `6800` | aria2 RPC,只在 docker 内部网络里被 PocketDrive 调用 | ❌ 不对外暴露,也无需映射 |

- **网页和 WebDAV 同端口没问题**:WebDAV 只是同一个 HTTP 服务下的 `/dav/` 路径,
  底层同样是标准 HTTP(ServeContent:流式 + Range + 条件请求)。Cloudreve 等项目
  拆端口通常是因为 WebDAV 由独立进程承载或想单独做 TLS/鉴权,并非协议要求。
- **离线下载(http/磁力/种子)全部是出站连接**,不需要额外开端口;只有 BT 想要
  更好的连通性时才放行 6888。

## 配置(环境变量)

| 变量 | 默认 | 说明 |
|---|---|---|
| `POCKETDRIVE_ADDR` | `:16688` | 监听地址 |
| `POCKETDRIVE_DATA_DIR` | `./data` | 网盘根目录 |
| `POCKETDRIVE_DB` | `./pocketdrive.db` | SQLite 路径(放数据目录外,避免出现在网盘里) |
| `POCKETDRIVE_ADMIN_USER` | `admin` | 管理员用户名 |
| `POCKETDRIVE_ADMIN_PASSWORD` | 随机生成 | 初始密码,之后在设置页修改(存库) |
| `POCKETDRIVE_ARIA2_RPC` | `http://127.0.0.1:6800/jsonrpc` | aria2 的 JSON-RPC 地址。官方 compose 里填 `http://aria2:6800/jsonrpc` 指向 aria2 容器 |
| `POCKETDRIVE_ARIA2_SECRET` | 必填 | aria2 RPC 密钥,必须和 aria2 容器的 `RPC_SECRET` 填成同一个随机值,不要使用公开示例密钥 |
| `POCKETDRIVE_ARIA2_DATA_DIR` | 同 DATA_DIR | aria2 进程视角的数据目录路径。两个容器要把网盘目录挂成同一个路径,否则下载完的文件在网盘里找不到 |

## 本机开发(Windows)

本机开发只需确保 Go、Node.js 和 ffmpeg 可用（安装后需重启终端）：

```powershell
winget install Gyan.FFmpeg.Shared
ffmpeg -version
```

如果不使用 `winget`，也可以把 `ffmpeg.exe` 所在目录加入系统 `PATH`。

```powershell
# 终端 1:后端(:16688)
./scripts/dev.ps1
# 终端 2:前端(:5173,代理 /api 与 /dav 到后端)
cd web; npm install; npm run dev
```

## 安全设计

- JWT 放 httpOnly cookie(SameSite=Strict),CSRF 校验 Origin/Sec-Fetch-Site
- 登录失败限流:同 IP 连错 5 次封 5 分钟;WebDAV Basic Auth,bcrypt + 成功凭据缓存
- 文件操作全部经 `os.Root`(Go 1.25+)防路径穿越/symlink 逃逸
- aria2 通过 RPC 通信;上传的 .torrent 做 bencode 头校验 + 16MB 上限
- Office 预览为纯前端渲染(docx-preview / SheetJS / pptx-preview 动态加载),后端不解析文档

## 常见问题

**离线下载报 `Download aborted.`,BT 报 `Failed to make the directory ..., cause: Permission denied`**

aria2 容器写不进网盘目录。`p3terx/aria2-pro` 镜像里 aria2c 固定以 `p3terx`
用户运行,不设 `PUID`/`PGID` 时它是 **65534(nobody)**;而 PocketDrive 以 root
建目录(`root:root 0755`),nobody 自然写不进去。`Download aborted.` 是 aria2
建文件失败时的外层文案,真正的原因不会经 RPC 传出来,所以看着像另一个问题。

修法:给 aria2 服务加上 `PUID=0` / `PGID=0` 后 `docker compose up -d`,
或者直接重跑一次安装脚本(见上面的「方式一:VPS 一键安装」)。

**复制按钮点了没反应/提示复制失败**

`navigator.clipboard` 只在 HTTPS(或 localhost)下存在,用 `http://IP:16688`
访问时是 undefined。已改为自动退回 `execCommand('copy')`;如果浏览器把它也禁了,
就只能手动选中链接复制,或者给站点配个域名 + HTTPS。
