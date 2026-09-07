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

上传下载全程流式,视频/音频播放走浏览器,不占内存;DOCX/PDF 预览在浏览器端渲染,
服务器零额外开销。
**视频只直连播放浏览器支持的格式**(mp4/webm 等);mkv/rmvb 在线预览不支持因为小内存装不下实时转码,这是刻意取舍。

## 功能

| 功能 |  |  |
|---|---|---|
| 文件管理 | WebDAV | 离线下载 |
| 断点续传 | 在线压缩/解压 | 全局搜索 |
| 在线预览 | Markdown 笔记 | 分享 |
| 黑夜模式 | 移动端响应式 |  |



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
        image: ghcr.io/lqlcj/pocketdrive-aria2:latest
        container_name: pocketdrive-aria2
        restart: unless-stopped
        stop_grace_period: 60s
        environment:
            - RPC_SECRET=请填一段随机的 RPC 密钥跟上面相同
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

aria2 现在使用本项目维护的 `ghcr.io/lqlcj/pocketdrive-aria2` 镜像,基于 Alpine
软件包构建,每周自动重建。从 `p3terx/aria2-pro` 升级时需要修改编排中的镜像名;
仅拉取旧镜像不会切换。一键脚本会在切换前停止旧服务并备份 aria2 配置。
手动升级、会话迁移及回退步骤见 [aria2 镜像说明](docker/aria2/README.md)。

### 卸载

```bash
cd /opt/pocketdrive

# 1. 只停服务,数据全部保留(之后 docker compose up -d 就能回来)
docker compose down

# 2. 连同容器卷一起删,再删掉整个目录 —— 网盘文件和配置库都会没
docker compose down -v
cd / && rm -rf /opt/pocketdrive

# 3. 顺手清掉镜像(可选)
docker rmi ghcr.io/lqlcj/pocketdrive:latest ghcr.io/lqlcj/pocketdrive-aria2:latest
```

删之前如需保留数据,请先复制编排目录里的 `data/` 和 `config/` 两个目录。

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

本机开发需要 Go 1.26.8+、Node.js 24 和 ffmpeg（安装后需重启终端）：

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
- DOCX 在禁止脚本的 sandbox iframe 内预览，禁用内嵌 HTML 和文档链接，并用 CSP 限制网络请求。
- 本机 WebDAV 上传先写临时文件，完整接收并落盘后才替换目标；中断或长度不符时保留原文件。
- 网页重命名、移动及 WebDAV MOVE/COPY 拒绝同名覆盖。S3 移动通过条件上传保护目标，会经过服务器中转；存储端需支持 `If-None-Match` 条件写入。
- 本机 WebDAV 删除进入回收站，可在网页恢复；回收站和上传临时文件不向 WebDAV 暴露。外部存储缺少统一回收站，因此拒绝 WebDAV DELETE；网页端外部存储的永久删除仍需谨慎操作。

安全回归验证：先运行 `npm --prefix web run build`，再运行 `go test ./...` 和
`go run golang.org/x/vuln/cmd/govulncheck@latest ./...`。
`node scripts/security-preview-smoke.mjs` 使用模拟接口检查恶意 DOCX 在桌面及手机视口的隔离，
默认使用本机 Edge；可用 `BROWSER_PATH` 指定 Chromium 浏览器路径。截图保存在 `web/shots/security/`，不读写网盘数据。

## 常见问题

**文件夹/下载好的东西过几天自己没了**

PocketDrive 自己只在两个地方定时删东西:回收站里超过 **30 天**的条目,以及
上传暂存目录里超过 **24 小时**的残片(只认自己生成的 32 位十六进制目录名,
别的一概不碰)。除此之外的删除都必须由人触发。

所以先看日志,一眼就能分清是不是它干的:

```bash
docker logs --since 168h pocketdrive | grep -E "\[删除\]|\[清理\]|\[消失\]"
```

* `[删除]` / `[清理]` —— PocketDrive 删的,行里写明了入口(网页、回收站、
  30 天到期、WebDAV、离线下载任务)
* 只有 `[消失]` 没有对应的 `[删除]` —— **不是 PocketDrive 删的**。哨兵每 30
  分钟给网盘拍一次快照,只报少掉的条目。这种情况去查三个地方:
  1. 编排里 `/data` 是不是绑到了宿主机目录(而不是匿名卷)。匿名卷会被
     `docker system prune --volumes` 和面板的「清理无用卷」一起带走:
     `docker inspect pocketdrive --format '{{json .Mounts}}'`
  2. 旧版第三方 aria2 容器有没有配「下载完成/停止就删文件」的钩子
     (新镜像不读取此文件,也没有配置删除钩子):
     `grep -nE '^\s*on-(download|bt-download)' config/aria2/aria2.conf`
  3. 服务器上的定时清理任务(面板的计划任务、`crontab -l`、`/etc/cron.daily/`)

另外别把 `POCKETDRIVE_DB` 指进网盘目录(如 `/data/pocketdrive.db`)。那样
上传暂存目录会变成网盘里一个看得见的普通文件夹,而它带自动清理。真这么配了
的话,启动日志里会有警告,内部目录会自动改用隐藏的 `.pocketdrive/`。

**离线下载报 `Download aborted.`,BT 报 `Failed to make the directory ..., cause: Permission denied`**

检查 aria2 和 PocketDrive 的 `/data` 是否挂载了同一个目录、是否可写,
以及两个容器的运行用户是否都有目录写权限。新镜像默认与主容器一样以 root
运行,不再使用 `PUID` / `PGID` 环境变量。`Download aborted.` 也可能有其他
原因,请结合 `docker logs pocketdrive-aria2` 查看具体错误。

**复制按钮点了没反应/提示复制失败**

`navigator.clipboard` 只在 HTTPS(或 localhost)下存在,用 `http://IP:16688`
访问时是 undefined。已改为自动退回 `execCommand('copy')`;如果浏览器把它也禁了,
就只能手动选中链接复制,或者给站点配个域名 + HTTPS。
