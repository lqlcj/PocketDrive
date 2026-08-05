# 多阶段构建:前端 → Go 单二进制 → 运行镜像
#
# 镜像里只带 ffmpeg(Alpine 官方包);aria2 跑在自己的容器里。
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

FROM alpine:3.22
# ffmpeg 用于缩略图抽帧,升级随主镜像进行。
RUN apk add --no-cache ca-certificates tzdata ffmpeg
COPY --from=gobuild /out/pocketdrive /usr/local/bin/pocketdrive
ENV POCKETDRIVE_ADDR=:16688 \
    POCKETDRIVE_DATA_DIR=/data \
    POCKETDRIVE_DB=/config/pocketdrive.db
VOLUME ["/data", "/config"]
# 只有网页+API+WebDAV;BT 端口属于 aria2 容器
EXPOSE 16688
ENTRYPOINT ["pocketdrive"]
