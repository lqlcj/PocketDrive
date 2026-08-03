# 多阶段构建:前端 → Go 单二进制 → 运行镜像(ffmpeg + yt-dlp;aria2 由 sidecar 容器提供)
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

FROM alpine:3.22
ARG TARGETARCH
# ffmpeg 仅用于 yt-dlp 流合并(remux),不做转码
# yt-dlp 用官方 musl 独立二进制:支持网页设置里的 -U 一键自更新
RUN apk add --no-cache ca-certificates tzdata ffmpeg curl \
    && case "$TARGETARCH" in \
         arm64) YTDLP=yt-dlp_musllinux_aarch64 ;; \
         *)     YTDLP=yt-dlp_musllinux ;; \
       esac \
    && curl -fsSL "https://github.com/yt-dlp/yt-dlp/releases/latest/download/${YTDLP}" \
         -o /usr/local/bin/yt-dlp \
    && chmod +x /usr/local/bin/yt-dlp \
    && apk del curl
COPY --from=gobuild /out/pocketdrive /usr/local/bin/pocketdrive
ENV POCKETDRIVE_ADDR=:8080 \
    POCKETDRIVE_DATA_DIR=/data \
    POCKETDRIVE_DB=/config/pocketdrive.db
VOLUME ["/data", "/config"]
EXPOSE 8080
ENTRYPOINT ["pocketdrive"]
