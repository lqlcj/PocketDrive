#!/usr/bin/env bash
# PocketDrive 一键安装脚本(Linux VPS,docker compose 方案)
#   curl -fsSL https://raw.githubusercontent.com/lqlcj/PocketDrive/main/scripts/install.sh | bash
# 重复执行 = 拉取新镜像升级,数据与密码不动。
set -euo pipefail

DIR=/opt/pocketdrive
IMAGE=ghcr.io/lqlcj/pocketdrive:latest
ARIA2_IMAGE=ghcr.io/lqlcj/pocketdrive:aria2-latest

say()  { echo -e "\033[1;32m[PocketDrive]\033[0m $*"; }
warn() { echo -e "\033[1;33m[PocketDrive]\033[0m $*"; }
die()  { echo -e "\033[1;31m[PocketDrive]\033[0m $*" >&2; exit 1; }

[ "$(uname -s)" = "Linux" ] || die "本脚本仅支持 Linux VPS"
[ "$(id -u)" = "0" ] || die "请用 root 运行(或 sudo bash)"

# ---- docker ----
if ! command -v docker >/dev/null 2>&1; then
    say "未检测到 docker,自动安装(get.docker.com)…"
    curl -fsSL https://get.docker.com | sh
    systemctl enable --now docker
fi
docker compose version >/dev/null 2>&1 || die "docker compose 插件不可用,请先安装 docker-compose-plugin"

# ---- 目录与配置 ----
mkdir -p "$DIR/data" "$DIR/config" "$DIR/config/aria2"
cd "$DIR"

rand() { head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n' | head -c "$1"; }

if [ ! -f .env ]; then
    ADMIN_PASS=$(rand 16)
    ARIA2_SECRET=$(rand 24)
    cat > .env <<EOF
POCKETDRIVE_ADMIN_PASSWORD=$ADMIN_PASS
ARIA2_SECRET=$ARIA2_SECRET
EOF
    chmod 600 .env
    say "已生成随机密码,保存在 $DIR/.env"
else
    say "检测到已有配置,沿用原密码(升级模式)"
    # 有一版把 aria2 内置在主容器里、不需要这个密钥;从那一版升上来要补一个
    if ! grep -q '^ARIA2_SECRET=' .env; then
        echo "ARIA2_SECRET=$(rand 24)" >> .env
        say "已补写 aria2 RPC 密钥"
    fi
fi
cat > docker-compose.yml <<EOF
services:
    pocketdrive:
        image: $IMAGE
        container_name: pocketdrive
        restart: unless-stopped
        init: true
        ports:
            - '16688:16688'
        environment:
            - POCKETDRIVE_DATA_DIR=/data
            - POCKETDRIVE_DB=/config/pocketdrive.db
            - POCKETDRIVE_ADMIN_USER=admin
            - POCKETDRIVE_ADMIN_PASSWORD=\${POCKETDRIVE_ADMIN_PASSWORD}
            - POCKETDRIVE_ARIA2_RPC=http://aria2:6800/jsonrpc
            - POCKETDRIVE_ARIA2_SECRET=\${ARIA2_SECRET}
            - POCKETDRIVE_ARIA2_DATA_DIR=/data
        volumes:
            - ./data:/data
            - ./config:/config
        depends_on:
            aria2:
                condition: service_healthy
    aria2:
        # 项目自维护镜像:aria2 二进制来自 Alpine 官方仓库,每周重建并冒烟测试
        image: $ARIA2_IMAGE
        container_name: pocketdrive-aria2
        restart: unless-stopped
        stop_grace_period: 30s
        environment:
            - RPC_SECRET=\${ARIA2_SECRET}
            - LISTEN_PORT=6888
            - MAX_CONCURRENT_DOWNLOADS=3
            - BT_MAX_PEERS=\${ARIA2_BT_MAX_PEERS:-55}
            - DISK_CACHE=\${ARIA2_DISK_CACHE:-64M}
            - ENABLE_DHT6=\${ARIA2_ENABLE_DHT6:-false}
            - UMASK_SET=022
            - TZ=\${TZ:-Asia/Shanghai}
        volumes:
            - ./data:/data
            # 继续使用旧版的 session/DHT 目录,升级不会丢未完成任务
            - ./config/aria2:/config
        ports:
            - '6888:6888'
            - '6888:6888/udp'

EOF

say "拉取镜像并启动…"
docker compose pull
docker compose up -d

say "等待原版 aria2 通过健康检查…"
ARIA2_HEALTH=starting
i=0
while [ "$i" -lt 45 ]; do
    ARIA2_HEALTH=$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' pocketdrive-aria2 2>/dev/null || true)
    [ "$ARIA2_HEALTH" = healthy ] && break
    [ "$ARIA2_HEALTH" = unhealthy ] && break
    i=$((i + 1))
    sleep 2
done
if [ "$ARIA2_HEALTH" != healthy ]; then
    docker compose logs --tail=100 aria2 >&2 || true
    die "aria2 启动检查失败(status=$ARIA2_HEALTH),请把以上日志发给维护者"
fi
ARIA2_VERSION=$(docker compose exec -T aria2 aria2c --version 2>/dev/null | sed -n '1p' || true)
[ -n "$ARIA2_VERSION" ] && say "aria2 已就绪:$ARIA2_VERSION"

IP=$(hostname -I 2>/dev/null | awk '{print $1}')
[ -n "$IP" ] || IP="服务器IP"

echo
say "安装完成!"
echo   "  网页地址 : http://$IP:16688"
echo   "  WebDAV   : http://$IP:16688/dav/(账号密码与网页相同)"
echo   "  用户名   : admin"
echo   "  密码     : $(grep POCKETDRIVE_ADMIN_PASSWORD .env | cut -d= -f2)"
echo   "  数据目录 : $DIR/data"
echo
warn "防火墙/安全组记得放行:16688/tcp(网页+WebDAV);6888/tcp+udp(BT 可选,不开也能下载只是慢)"
warn "升级:重跑本脚本,或 cd $DIR && docker compose pull && docker compose up -d"
say "升级请在 1Panel 中拉取最新镜像并重建编排"
warn "卸载:cd $DIR && docker compose down(数据保留在 data/)"
