#!/usr/bin/env bash
# PocketDrive 一键安装脚本(Linux VPS,docker compose 方案)
#   curl -fsSL https://raw.githubusercontent.com/lqlcj/PocketDrive/main/scripts/install.sh | bash
# 重复执行 = 拉取新镜像升级,数据与密码不动。
set -euo pipefail

DIR=/opt/pocketdrive
IMAGE=ghcr.io/lqlcj/pocketdrive:latest

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
if ! grep -q '^POCKETDRIVE_UPDATER_TOKEN=' .env; then
    echo "POCKETDRIVE_UPDATER_TOKEN=$(rand 32)" >> .env
    chmod 600 .env
    say "已生成内部升级服务密钥"
fi

cat > docker-compose.yml <<EOF
services:
    pocketdrive:
        image: $IMAGE
        container_name: pocketdrive
        labels:
            com.centurylinklabs.watchtower.enable: "true"
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
            - POCKETDRIVE_UPDATER_URL=http://pocketdrive-updater:8080
            - POCKETDRIVE_UPDATER_TOKEN=\${POCKETDRIVE_UPDATER_TOKEN}
        volumes:
            - ./data:/data
            - ./config:/config
        depends_on:
            - aria2
        networks:
            - default
            - update-internal

    pocketdrive-updater:
        image: containrrr/watchtower:1.7.1
        container_name: pocketdrive-updater
        labels:
            com.centurylinklabs.watchtower.enable: "false"
        restart: unless-stopped
        command: --http-api-update --http-api-periodic-polls=false --label-enable --cleanup
        environment:
            - WATCHTOWER_HTTP_API_TOKEN=\${POCKETDRIVE_UPDATER_TOKEN}
            - WATCHTOWER_HTTP_API_METRICS=false
            - DOCKER_API_VERSION=1.40
        volumes:
            - /var/run/docker.sock:/var/run/docker.sock:ro
        read_only: true
        cap_drop: [ALL]
        security_opt: [no-new-privileges:true]
        networks:
            - update-internal

    aria2:
        image: p3terx/aria2-pro
        container_name: pocketdrive-aria2
        restart: unless-stopped
        environment:
            - RPC_SECRET=\${ARIA2_SECRET}
            - LISTEN_PORT=6888
            - MAX_CONCURRENT_DOWNLOADS=3
            # 镜像默认让 aria2c 以 nobody(65534)运行,写不进 PocketDrive
            # 以 root 建的目录;统一成 root 才能共享 /data
            - PUID=0
            - PGID=0
            - UMASK_SET=022
        volumes:
            - ./data:/data
            - ./config/aria2:/config
        ports:
            - '6888:6888'
            - '6888:6888/udp'

networks:
    update-internal:
        internal: true
EOF

say "拉取镜像并启动…"
docker compose pull
docker compose up -d

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
warn "yt-dlp 单独在网页「设置 → 组件状态」里升级即可,不用动服务器"
warn "卸载:cd $DIR && docker compose down(数据保留在 data/)"
