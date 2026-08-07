#!/usr/bin/env bash
# 对 PocketDrive 自维护 aria2 镜像做真实冒烟测试：
#   - 健康检查与 RPC 密钥
#   - pause-metadata 选项可用
#   - HTTPS 文件能实际写入共享 /data
set -euo pipefail

IMAGE=${1:-pocketdrive-aria2:test}
NAME="pocketdrive-aria2-smoke-$$"
ROOT=$(mktemp -d)
# 覆盖旧部署中常见的特殊字符密钥，防止健康检查 JSON 转义回归。
SECRET='smoke_test_2026!@+='

cleanup() {
    docker rm -f "$NAME" >/dev/null 2>&1 || true
    rm -rf -- "$ROOT"
}
trap cleanup EXIT

mkdir -p "$ROOT/data" "$ROOT/config"
docker run -d --name "$NAME" \
    -e "RPC_SECRET=$SECRET" \
    -v "$ROOT/data:/data" \
    -v "$ROOT/config:/config" \
    "$IMAGE" >/dev/null

for _ in $(seq 1 60); do
    status=$(docker inspect -f '{{.State.Health.Status}}' "$NAME" 2>/dev/null || true)
    [ "$status" = healthy ] && break
    [ "$status" = unhealthy ] && break
    sleep 1
done

if [ "${status:-}" != healthy ]; then
    docker logs "$NAME" >&2 || true
    echo "aria2 容器未通过健康检查(status=${status:-unknown})" >&2
    exit 1
fi

rpc() {
    docker exec "$NAME" curl -fsS -H 'Content-Type: application/json' \
        --data "$1" http://127.0.0.1:6800/jsonrpc
}

version=$(rpc "{\"jsonrpc\":\"2.0\",\"id\":\"v\",\"method\":\"aria2.getVersion\",\"params\":[\"token:$SECRET\"]}")
printf '%s' "$version" | grep -q '"version"' || {
    echo "getVersion 返回异常: $version" >&2
    exit 1
}

magnet='magnet:?xt=urn:btih:0000000000000000000000000000000000000000'
meta=$(rpc "{\"jsonrpc\":\"2.0\",\"id\":\"m\",\"method\":\"aria2.addUri\",\"params\":[\"token:$SECRET\",[\"$magnet\"],{\"pause\":\"true\",\"pause-metadata\":\"true\"}]}")
meta_gid=$(printf '%s' "$meta" | sed -n 's/.*"result":"\([^"]*\)".*/\1/p')
[ -n "$meta_gid" ] || {
    echo "pause-metadata 磁力任务添加失败: $meta" >&2
    exit 1
}
rpc "{\"jsonrpc\":\"2.0\",\"id\":\"rm\",\"method\":\"aria2.remove\",\"params\":[\"token:$SECRET\",\"$meta_gid\"]}" >/dev/null

add=$(rpc "{\"jsonrpc\":\"2.0\",\"id\":\"a\",\"method\":\"aria2.addUri\",\"params\":[\"token:$SECRET\",[\"https://www.example.com/\"],{\"dir\":\"/data\",\"out\":\"aria2-smoke.html\",\"allow-overwrite\":\"true\"}]}")
gid=$(printf '%s' "$add" | sed -n 's/.*"result":"\([^"]*\)".*/\1/p')
[ -n "$gid" ] || {
    echo "HTTPS 任务添加失败: $add" >&2
    exit 1
}

result=''
for _ in $(seq 1 30); do
    result=$(rpc "{\"jsonrpc\":\"2.0\",\"id\":\"s\",\"method\":\"aria2.tellStatus\",\"params\":[\"token:$SECRET\",\"$gid\",[\"status\",\"errorMessage\"]]}")
    printf '%s' "$result" | grep -q '"status":"complete"' && break
    if printf '%s' "$result" | grep -q '"status":"error"'; then
        echo "HTTPS 下载失败: $result" >&2
        exit 1
    fi
    sleep 1
done

printf '%s' "$result" | grep -q '"status":"complete"' || {
    echo "HTTPS 下载超时: $result" >&2
    exit 1
}
[ -s "$ROOT/data/aria2-smoke.html" ] || {
    echo "aria2 未把文件写入共享 /data" >&2
    exit 1
}
[ -f "$ROOT/config/aria2.session" ] || {
    echo "aria2 会话文件未创建" >&2
    exit 1
}

echo "aria2 镜像冒烟测试通过: $version"
