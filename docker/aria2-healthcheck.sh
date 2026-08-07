#!/bin/sh
set -eu

: "${RPC_SECRET:?必须设置 RPC_SECRET}"
RPC_PORT=${RPC_PORT:-6800}

# jq 负责 JSON 转义，因此从旧镜像升级时可继续使用包含常见特殊字符的
# RPC 密钥。只有拿同一密钥成功调用 getVersion 才算 healthy。
payload=$(jq -nc --arg token "token:${RPC_SECRET}" \
    '{jsonrpc:"2.0",id:"health",method:"aria2.getVersion",params:[$token]}')

curl -fsS -H 'Content-Type: application/json' \
    --data "$payload" \
    "http://127.0.0.1:${RPC_PORT}/jsonrpc" | grep -q '"result"'
