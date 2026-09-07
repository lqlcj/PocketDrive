#!/bin/sh
set -eu

: "${RPC_SECRET:?RPC_SECRET must be set}"
# A secret is one config line; reject line breaks instead of injecting options.
case "$RPC_SECRET" in
    *"$(printf '\r')"*|*'
'*) echo 'RPC_SECRET must not contain line breaks' >&2; exit 1 ;;
esac

umask 022
mkdir -p /data /config
touch /config/aria2.session
# Keep the secret out of process arguments and the persistent shared config.
umask 077
runtime_config=$(mktemp /tmp/aria2.XXXXXX)
cat /etc/aria2/aria2.conf > "$runtime_config"
printf '\nrpc-secret=%s\n' "$RPC_SECRET" >> "$runtime_config"
unset RPC_SECRET
umask 022
exec aria2c --conf-path="$runtime_config"
