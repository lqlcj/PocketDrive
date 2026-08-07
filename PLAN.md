# PocketDrive(原 lpanel)v0.1 — 动森风格个人网盘

> 部署目标:2G 内存 VPS,Docker 部署。单管理员,纯网盘定位(不含 VPS 系统操作面板)。

## 一、定位与范围

自托管个人网盘,仪表盘样式,动森(Animal Crossing)视觉风格。

**v0.1 包含**:
1. 单管理员认证(Web 登录 + WebDAV Basic Auth 同一账号)
2. 文件管理:列表 / 上传 / 下载 / 删除 / 重命名 / 新建文件夹 / 移动
3. WebDAV 服务(`/dav/`),**暴露整个网盘根目录**——客户端自选任意子文件夹(如歌曲目录)直连
4. 在线预览:Markdown、图片、视频、音频、纯文本/代码
5. 离线下载:HTTP(S) 直链 + 磁力/BT(aria2)
7. 仪表盘首页:存储用量、下载任务进度、最近文件、快捷入口

**数据模型**:整个网盘就是一个数据目录(如 `/data`),Web 文件管理、WebDAV、aria2 全部读写同一目录,用户自行建文件夹组织。

**v0.1 不做**(候选 v0.2):多用户、分享链接、视频转码、磁盘配额、.torrent 文件上传、回收站。

## 二、技术栈

| 层 | 选型 | 理由 |
|---|---|---|
| 后端 | Go 1.25+,标准库 net/http | 单二进制、常驻内存 ~40-80MB,交叉编译 amd64/arm64 |
| 数据库 | SQLite(glebarez/sqlite 纯 Go 驱动)+ GORM | CGO_ENABLED=0 可静态编译;WAL + `SetMaxOpenConns(1)` |
| WebDAV | golang.org/x/net/webdav | 标准库级实现,直接挂数据目录 |
| 离线下载 | 项目自维护的原版 aria2 sidecar(JSON-RPC) | Alpine 官方包,直链和 BT 一个进程全包,~30-100MB |
| 前端 | React 18 + TypeScript + Vite | — |
| UI 库 | animal-island-ui v1.4+ | 31 组件,活跃维护;**CC-BY-NC-4.0 禁商用,私用 OK** |
| Markdown | react-markdown + remark-gfm | — |
| 部署 | docker-compose(PocketDrive + aria2 两个容器) | 两个 GHCR 镜像均由同一工作流构建和测试 |

## 三、内存预算(2G VPS)

| 组件 | 常驻内存 | 说明 |
|---|---|---|
| lpanel(Go) | 40-80MB | 上传/下载全部流式 io.Copy,不整文件进内存 |
| aria2 | 30-100MB | 限制 `bt-max-peers=55`、`max-concurrent-downloads=3`,会话/DHT 持久化 |
| SQLite | 内嵌 | 无独立进程 |

合计 <200MB,余量充足。视频/音频播放走 `http.ServeContent`(Range 请求),内存开销近零。
**视频只直连播放浏览器支持的格式**(mp4/webm/H.264/AAC);mkv/rmvb 等提供下载不转码。

## 四、架构决定(含上一版验证过的经验)

- **go.mod 在仓库根**;`web/embed.go` 与 `web/dist` 同目录(go:embed 不能跨包)
- **认证**:JWT 放 httpOnly cookie(SameSite=Strict)+ 兼收 `Authorization: Bearer`;CSRF 中间件校验 Origin/Sec-Fetch-Site;WebDAV 走 Basic Auth(bcrypt 校验,带简单速率限制)
- **路径安全**:所有文件操作经 `os.Root`(Go 1.25+)防穿越/symlink 逃逸;API 路径一律 slash 分隔的相对路径
- **aria2**:磁力任务 followedBy gid 迁移要处理;任务终态回写 DB(aria2 重启不丢历史);aria2 不可达时 API 返回 degraded 降级而非 500
- **GORM 列名**:缩写字段(GID 等)显式 `gorm:"column:gid"`,避免命名策略拆成 g_id
- **单管理员**:用户名/密码哈希存 config.yaml 或环境变量初始化,DB 只存任务/元数据

## 五、目录结构

```
pocketdrive/
├── go.mod
├── cmd/pocketdrive/main.go
├── internal/
│   ├── config/        # config.yaml + 环境变量
│   ├── auth/          # JWT / Basic Auth / CSRF
│   ├── files/         # 文件 CRUD(os.Root)
│   ├── webdav/        # /dav/ handler
│   ├── aria2/         # JSON-RPC client + 任务同步
│   └── server/        # 路由、中间件、embed 静态资源
├── web/               # React 前端(Vite)
│   ├── embed.go
│   └── src/
│       ├── pages/     # Dashboard / Files / Downloads / Settings
│       └── components/# 手写动森风格补充组件(侧边栏/上传/面包屑/右键菜单)
├── docker/
│   ├── Dockerfile     # 多阶段:node 构建前端 → go 构建 → scratch/alpine
│   └── docker-compose.yml   # lpanel + aria2(p2p 端口映射)
└── PLAN.md
```

## 六、前端页面(仪表盘布局)

左侧动森风格侧边栏:**主页 / 文件 / 离线下载 / 视频下载 / 设置**;主区卡片式布局,移动端响应式(侧边栏收为抽屉)。

- **主页(仪表盘)**:存储用量进度卡(Progress)、活跃下载任务卡、最近文件卡、快捷操作(上传/新建下载)
- **文件**:面包屑 + 列表/网格切换(Table/Card)、拖拽上传、右键菜单(重命名/移动/删除)、点击进预览
- **预览**:Modal 或独立层 —— md 渲染 / 图片(可左右切换)/ video、audio 标签直连
- **离线下载**:粘贴直链或磁力新建任务;任务列表(进度条 1s 轮询)、暂停/恢复/删除
- **设置**:修改密码、WebDAV 地址展示(方便手机端抄)、aria2 连接状态

animal-island-ui 现成可用:Button/Card/Modal/Table/Tabs/Progress/Drawer/Notification/Input/Select/Loading/Skeleton/Tag/Tooltip。
需手写(照它的风格):侧边栏导航、上传组件、面包屑、右键菜单、文件类型图标。

## 七、里程碑

- **M0 脚手架**:Go 服务 + Vite + animal-island-ui 跑通,go:embed 单二进制,docker-compose 骨架
- **M1 认证 + 文件管理**:登录页、JWT/CSRF、文件 CRUD API + 文件页(上传/下载/删/改名/建目录/移动)
- **M2 WebDAV**:/dav/ + Basic Auth,用手机播放器实测连通
- **M3 预览**:markdown / 图片 / 视频 / 音频 / 文本
- **M4 下载中心**:aria2 集成(直链 + 磁力),任务管理页,mock 回归测试
- **M5 仪表盘 + 部署**:主页仪表盘、Docker 镜像、amd64/arm64 交叉编译、README

## 八、Windows 本机开发注意(上一版踩过的坑)

- curl/node 是原生程序:argv 中文会转 GBK 乱码(测中文用 JSON \u 转义或写文件);不认 MSYS /tmp
- Vite 显式 `host: "127.0.0.1"`(默认绑 IPv6 导致 curl 连不上)
- 开发期前端必须走 Vite 代理(CSRF Origin 校验)
- PowerShell 下无 `VAR=x cmd` 语法,用 scripts/dev.ps1(`$env:` 方式)
