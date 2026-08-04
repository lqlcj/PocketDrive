# 多阶段构建:前端 → Go 单二进制 → 运行镜像
# aria2 / yt-dlp / ffmpeg 三个组件都由 PocketDrive 自己管理(装在 /config 卷内),
# 网页设置页可各自升级,且升级结果跨容器重启保留。镜像里放一份内置副本作为
# 首次启动的基线,离线环境不联网也能直接用。
FROM node:24-alpine AS webbuild
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS gobuild
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=webbuild /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/pocketdrive ./cmd/pocketdrive

# 组件内置副本:各取官方/主流静态构建,不依赖镜像里的动态库
FROM alpine:3.22 AS bins
ARG TARGETARCH
RUN apk add --no-cache curl unzip xz tar \
    && mkdir -p /bundled \
    && case "$TARGETARCH" in \
         arm64) YTDLP=yt-dlp_musllinux_aarch64; \
                ARIA=aria2-aarch64-linux-musl_static.zip; \
                FF=ffmpeg-master-latest-linuxarm64-gpl.tar.xz ;; \
         *)     YTDLP=yt-dlp_musllinux; \
                ARIA=aria2-x86_64-linux-musl_static.zip; \
                FF=ffmpeg-master-latest-linux64-gpl.tar.xz ;; \
       esac \
    && curl -fsSL "https://github.com/yt-dlp/yt-dlp/releases/latest/download/${YTDLP}" \
         -o /bundled/yt-dlp \
    && curl -fsSL "https://github.com/abcfy2/aria2-static-build/releases/latest/download/${ARIA}" \
         -o /tmp/aria2.zip \
    && unzip -j -o /tmp/aria2.zip '*/aria2c' -d /bundled \
    && curl -fsSL "https://github.com/BtbN/FFmpeg-Builds/releases/latest/download/${FF}" \
         -o /tmp/ffmpeg.tar.xz \
    && tar -xJf /tmp/ffmpeg.tar.xz -C /tmp \
    && find /tmp -type f -name ffmpeg -path '*/bin/*' -exec cp {} /bundled/ffmpeg \; \
    && chmod +x /bundled/* \
    && rm -rf /tmp/*

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
COPY --from=bins /bundled /opt/pocketdrive/bin
COPY --from=gobuild /out/pocketdrive /usr/local/bin/pocketdrive
ENV POCKETDRIVE_ADDR=:16688 \
    POCKETDRIVE_DATA_DIR=/data \
    POCKETDRIVE_DB=/config/pocketdrive.db \
    POCKETDRIVE_BIN_DIR=/config/bin \
    POCKETDRIVE_BIN_BUNDLED=/opt/pocketdrive/bin
VOLUME ["/data", "/config"]
# 16688 网页+API+WebDAV;6888 是 aria2 的 BT 监听端口(TCP+UDP,可选)
EXPOSE 16688 6888 6888/udp
ENTRYPOINT ["pocketdrive"]
