#!/system/bin/sh
# Start the local MCP file bridge once the system has finished booting.
until [ "$(getprop sys.boot_completed)" = 1 ]; do
  sleep 2
done

MODDIR=${0%/*}
chmod 0755 "$MODDIR/local-bridge-android"

# The bridge token lives inside the module dir (created at install by
# customize.sh), so it is removed when the module is uninstalled. Fallback-
# generate it here if missing (e.g. a manager that skipped customize.sh) so auth
# is always on. BRIDGE_TOKEN gates access -- on Android any local app can reach
# 127.0.0.1 and this server runs as root, so a token is required. The SAME token
# must be set in Miclaw's local MCP config as header  Authorization: Bearer <token>.
TOKEN_FILE="$MODDIR/token"
if [ ! -s "$TOKEN_FILE" ]; then
  TOKEN="$(cat /proc/sys/kernel/random/uuid)$(cat /proc/sys/kernel/random/uuid)"
  echo "$TOKEN" | tr -d '-' > "$TOKEN_FILE"
  chmod 600 "$TOKEN_FILE"
fi
BRIDGE_TOKEN="$(cat "$TOKEN_FILE")"

# Backend config: the gateway proxies the sandbox tools to your remote sandbox-mcp
# server and also uses this URL+token for push_file/pull_file. Edit sandbox.conf
# (created at install by customize.sh) to set SANDBOX_BASE_URL and SANDBOX_TOKEN.
# If unset, initialize() fails on purpose so Miclaw won't load a half-broken MCP.
CONF="$MODDIR/sandbox.conf"
[ -s "$CONF" ] && . "$CONF"

# Native Android build: uses the OS DNS resolver and CA store, no extra config.
# LOCAL_MCP_ADDR is where it listens (Miclaw connects to http://127.0.0.1:8765/mcp).
BRIDGE_TOKEN="$BRIDGE_TOKEN" \
LOCAL_MCP_ADDR=127.0.0.1:8765 \
SANDBOX_BASE_URL="$SANDBOX_BASE_URL" \
SANDBOX_TOKEN="$SANDBOX_TOKEN" \
  "$MODDIR/local-bridge-android" > /data/local/tmp/local-bridge.log 2>&1 &
