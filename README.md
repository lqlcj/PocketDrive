# PocketDrive — 口袋网盘

<img width="1983" height="793" alt="hengfu" src="https://github.com/user-attachments/assets/84fd00e9-185f-47ae-a4d6-99fec2c3c124" />

自托管个人网盘,专为小内存 VPS设计。前端 React,后端 Go ,SQLite 存储。

---

你的 512MB、1GB 内存的小鸡，是不是只够装个探针然后吃灰？送的存储不用又浪费？

市面上优秀网盘很多，可惜大多太重了——要装数据库、要装缓存、还带一堆插件，咱们小鸡根本扛不住。于是就有了这款轻量网盘。

虽然轻量，但功能不减。存储采用 SQLite，支持 WebDAV，内置 aria2 离线下载，还能自动更新 Tracker。文件预览直接调用浏览器能力，不堆插件。

也不打算做多用户和多客户端，一切目标都是轻量，让小鸡也能用，不能让它只配装探针。响应式已做好，移动端添加到桌面用 PWA 体验也很好。

---
夜间模式:
<img width="1537" height="882" alt="黑夜模式-min" src="https://github.com/user-attachments/assets/9f324de5-9403-4683-9467-741b39388e04" />
日间模式:
<img width="1550" height="882" alt="日间模式1-min" src="https://github.com/user-attachments/assets/98fa6c2e-d215-49c2-aa0c-cc50bff203cb" />

## 资源占用情况

| 组件 | 常驻内存 |
|---|---|
| PocketDrive(Go,含前端) | ~40-80MB |
| aria2 | ~30-100MB |

两个进程常驻内存还没100M,RN,CC的小鸡再也不用吃灰了,线路鸡也可以利用起来了.
<img width="917" height="330" alt="内存-min" src="https://github.com/user-attachments/assets/ad41ac05-d3ad-4856-a115-ec60fbe8116c" />

上传下载全程流式,视频/音频播放走浏览器,不占内存;Office/PDF 预览在浏览器端渲染,
服务器零额外开销。
**视频只直连播放浏览器支持的格式**(mp4/webm 等);mkv/rmvb 在线预览不支持因为小内存装不下实时转码,这是刻意取舍。

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
拉起 PocketDrive 和项目自维护的原版 aria2 容器 → 等待健康检查通过并打印实际版本。
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
            aria2:
                condition: service_healthy
    aria2:
        image: ghcr.io/lqlcj/pocketdrive:aria2-latest
        container_name: pocketdrive-aria2
        restart: unless-stopped
        stop_grace_period: 30s
        environment:
            - RPC_SECRET=请填一段随机的 RPC 密钥跟上面相同
            - LISTEN_PORT=6888
            - MAX_CONCURRENT_DOWNLOADS=3
            - BT_MAX_PEERS=55
            - DISK_CACHE=64M
            # 有公网 IPv6 时可改成 true
            - ENABLE_DHT6=false
            - UMASK_SET=022
            - TZ=Asia/Shanghai
        volumes:
            - ./data:/data
            # 会话与 DHT 路由表持久化；从旧 p3terx 镜像升级也沿用此目录
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

这会同时更新 PocketDrive、ffmpeg 和原版 aria2 镜像，不会删除 `data/`、`config/`
中的数据。aria2 镜像由本项目每周从 Alpine 官方仓库无缓存重建，发布前会验证
RPC、`pause-metadata` 和一次真实 HTTPS 下载。

### 卸载

```bash
cd /opt/pocketdrive

# 1. 只停服务,数据全部保留(之后 docker compose up -d 就能回来)
docker compose down

# 2. 连同容器卷一起删,再删掉整个目录 —— 网盘文件和配置库都会没
docker compose down -v
cd / && rm -rf /opt/pocketdrive

# 3. 顺手清掉镜像(可选)
docker rmi ghcr.io/lqlcj/pocketdrive:latest ghcr.io/lqlcj/pocketdrive:aria2-latest
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

## aria2 与 Tracker 维护

- aria2 上游没有官方 Docker 镜像。`ghcr.io/lqlcj/pocketdrive:aria2-latest`
  不使用 Aria2-Pro 等第三方配置层，直接从 Alpine 当前稳定版的官方 community
  仓库安装上游 aria2，并只保留 PocketDrive 必需的 RPC、会话、DHT 和资源限制参数。
  它复用现有公开 GHCR 包的独立 `aria2-*` 标签，因此 VPS 无需额外登录仓库。
- GitHub Actions 每周重新拉取 Alpine 基础镜像和 aria2 包。新镜像发布前会启动
  容器、校验 RPC 密钥、验证 `pause-metadata`，再实际下载一个 HTTPS 文件到
  `/data`；任一步失败都不会覆盖 `latest`。
- 原版 aria2 只会“使用 Tracker”，不会定时下载公共列表。PocketDrive 会每日
  从 GitHub Raw、jsDelivr、GitHub Pages 三个镜像依次更新；全部失败时继续使用
  上一次成功缓存，并在 **下载 → 下载设置** 显示错误。首次缓存尚未建立时会先用
  3 条内置启动 Tracker，避免刚部署就添加的磁力只能碰运气等待 DHT。
- 下载设置里可以关闭公共列表，或添加最多 100 条自定义 `http/https/udp`
  Tracker（每行一条）。自定义项会去重，且在关闭自动更新后仍然生效。

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

aria2 容器可选变量：

| 变量 | 默认 | 说明 |
|---|---|---|
| `LISTEN_PORT` | `6888` | BT 与 IPv4 DHT 监听端口，需同时映射 TCP/UDP |
| `MAX_CONCURRENT_DOWNLOADS` | `3` | 启动初值；PocketDrive 连接后会用网页设置覆盖 |
| `BT_MAX_PEERS` | `55` | 单个 BT 任务最大 Peer 数，兼顾连接率和小内存 VPS |
| `DISK_CACHE` | `64M` | aria2 磁盘缓存 |
| `ENABLE_DHT6` | `false` | 服务器有可用公网 IPv6 时可启用 IPv6 DHT |
| `UMASK_SET` | `022` | 新建文件权限掩码 |

使用本项目 compose 或一键安装脚本时，可在部署目录 `.env` 里写
`ARIA2_ENABLE_DHT6=true`、`ARIA2_BT_MAX_PEERS=80`、`ARIA2_DISK_CACHE=128M`；
编排会把它们映射到上表对应的容器变量。

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

通常是自行修改编排后，PocketDrive 与 aria2 没有把同一个宿主机目录挂到
`/data`，或修改了容器运行用户。项目维护镜像默认与 PocketDrive 使用相同权限，
不再需要旧版的 `PUID`/`PGID` 补丁。先运行 `docker compose logs aria2` 看原始原因，
再对照上面的官方编排恢复两处 `./data:/data`；不确定时直接重跑一键安装脚本。

**磁力一直显示“正在获取种子信息”**

上传 `.torrent` 时元数据已经在文件里；磁力只有哈希，必须从仍持有元数据的 Peer
获取。先在 **下载设置** 确认自动 Tracker 有列表，或手动点“立即更新”后移除并
重新添加该磁力，同时确认服务器允许 UDP 出站、6888 TCP/UDP 已放行。若
Tracker/DHT 都正常仍长期失败，通常是该磁力已经没人持有元数据，只能换
`.torrent` 文件或其他资源。

**复制按钮点了没反应/提示复制失败**

`navigator.clipboard` 只在 HTTPS(或 localhost)下存在,用 `http://IP:16688`
访问时是 undefined。已改为自动退回 `execCommand('copy')`;如果浏览器把它也禁了,
就只能手动选中链接复制,或者给站点配个域名 + HTTPS。
