# 多阶段构建:前端 → Go 单二进制 → 运行镜像
#
# 镜像里只带 ffmpeg(Alpine 官方包)和一份 yt-dlp 内置副本:
#   yt-dlp  首次启动复制到 /config/bin(volume 内),之后在网页里点一下
#           就能升级,升级结果跨容器重启保留
#   ffmpeg  随本镜像发布,docker compose pull 即可升级
#   aria2   跑在自己的容器里(见 docker-compose.yml),同样 pull 升级
FROM --platform=$BUILDPLATFORM node:24-alpine AS webbuild
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# 同样固定在构建机架构上:CGO 关着,交叉编译即可,不必让 npm/go 走 QEMU 模拟
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS gobuild
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=webbuild /src/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/pocketdrive ./cmd/pocketdrive

# yt-dlp 内置副本。跑在构建机架构上(只是下载,不执行),免得交叉构建
# arm64 时整个阶段走 QEMU 模拟。
FROM --platform=$BUILDPLATFORM alpine:3.22 AS ytdlp
ARG TARGETARCH
RUN apk add --no-cache curl \
    && mkdir -p /bundled \
    && case "$TARGETARCH" in \
         arm64) YTDLP=yt-dlp_musllinux_aarch64 ;; \
         *)     YTDLP=yt-dlp_musllinux ;; \
       esac \
    && curl -fsSL "https://github.com/yt-dlp/yt-dlp/releases/latest/download/${YTDLP}" \
         -o /bundled/yt-dlp \
    && chmod +x /bundled/yt-dlp

FROM alpine:3.22
# ffmpeg 用于 yt-dlp 流合并/remux；Node.js 用于 yt-dlp-ejs 解 YouTube
# 播放器挑战。yt-dlp 官方要求 Node >= 22，并且需要显式启用。
RUN apk add --no-cache ca-certificates tzdata ffmpeg nodejs
COPY --from=ytdlp /bundled /opt/pocketdrive/bin
COPY --from=gobuild /out/pocketdrive /usr/local/bin/pocketdrive
ENV POCKETDRIVE_ADDR=:16688 \
    POCKETDRIVE_DATA_DIR=/data \
    POCKETDRIVE_DB=/config/pocketdrive.db \
    POCKETDRIVE_BIN_DIR=/config/bin \
    POCKETDRIVE_BIN_BUNDLED=/opt/pocketdrive/bin
VOLUME ["/data", "/config"]
# 只有网页+API+WebDAV;BT 端口属于 aria2 容器
EXPOSE 16688
ENTRYPOINT ["pocketdrive"]
