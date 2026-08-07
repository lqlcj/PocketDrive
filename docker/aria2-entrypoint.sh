#!/bin/sh
set -eu

die() {
    echo "[PocketDrive aria2] $*" >&2
    exit 1
}

require_uint() {
    name=$1
    value=$2
    min=$3
    max=$4
    case "$value" in
        ''|*[!0-9]*) die "$name 必须是整数" ;;
    esac
    [ "$value" -ge "$min" ] && [ "$value" -le "$max" ] ||
        die "$name 必须在 $min-$max 之间"
}

: "${RPC_SECRET:?必须设置 RPC_SECRET}"

RPC_PORT=${RPC_PORT:-6800}
LISTEN_PORT=${LISTEN_PORT:-6888}
MAX_CONCURRENT_DOWNLOADS=${MAX_CONCURRENT_DOWNLOADS:-3}
BT_MAX_PEERS=${BT_MAX_PEERS:-55}
DISK_CACHE=${DISK_CACHE:-64M}
ENABLE_DHT6=${ENABLE_DHT6:-false}
UMASK_SET=${UMASK_SET:-022}
CONFIG_DIR=${ARIA2_CONFIG_DIR:-/config}
DATA_DIR=${ARIA2_DATA_DIR:-/data}

require_uint RPC_PORT "$RPC_PORT" 1 65535
require_uint LISTEN_PORT "$LISTEN_PORT" 1 65535
require_uint MAX_CONCURRENT_DOWNLOADS "$MAX_CONCURRENT_DOWNLOADS" 1 100
require_uint BT_MAX_PEERS "$BT_MAX_PEERS" 1 1000

case "$UMASK_SET" in
    [0-7][0-7][0-7]) ;;
    *) die "UMASK_SET 必须是三位八进制数，例如 022" ;;
esac
case "$ENABLE_DHT6" in
    true|false) ;;
    *) die "ENABLE_DHT6 只能是 true 或 false" ;;
esac

umask "$UMASK_SET"
mkdir -p "$CONFIG_DIR" "$DATA_DIR"
touch "$CONFIG_DIR/aria2.session"

set -- aria2c \
    --enable-rpc=true \
    --rpc-listen-all=true \
    --rpc-allow-origin-all=false \
    --rpc-listen-port="$RPC_PORT" \
    --rpc-secret="$RPC_SECRET" \
    --rpc-max-request-size=32M \
    --dir="$DATA_DIR" \
    --input-file="$CONFIG_DIR/aria2.session" \
    --save-session="$CONFIG_DIR/aria2.session" \
    --save-session-interval=30 \
    --force-save=true \
    --continue=true \
    --auto-save-interval=60 \
    --max-download-result=1000 \
    --max-concurrent-downloads="$MAX_CONCURRENT_DOWNLOADS" \
    --listen-port="$LISTEN_PORT" \
    --dht-listen-port="$LISTEN_PORT" \
    --enable-dht=true \
    --dht-file-path="$CONFIG_DIR/dht.dat" \
    --bt-max-peers="$BT_MAX_PEERS" \
    --enable-peer-exchange=true \
    --bt-enable-lpd=false \
    --bt-detach-seed-only=true \
    --seed-time=0 \
    --file-allocation=none \
    --disk-cache="$DISK_CACHE" \
    --check-certificate=true \
    --enable-color=false \
    --console-log-level=notice \
    --log=- \
    --log-level=notice

if [ "$ENABLE_DHT6" = true ]; then
    set -- "$@" \
        --enable-dht6=true \
        --dht-file-path6="$CONFIG_DIR/dht6.dat"
else
    set -- "$@" --enable-dht6=false
fi

echo "[PocketDrive aria2] 启动原版 $(aria2c --version | sed -n '1s/^aria2 version //p')，RPC :$RPC_PORT，BT :$LISTEN_PORT"
exec "$@"
