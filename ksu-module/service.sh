#!/system/bin/sh
# Start the local MCP file bridge once the system has finished booting.
until [ "$(getprop sys.boot_completed)" = 1 ]; do
  sleep 2
done

MODDIR=${0%/*}
chmod 0755 "$MODDIR/local-bridge-android"

# The bridge token is generated at install time by customize.sh and stored here,
# outside the module dir so it survives updates. Fallback-generate it if missing
# (e.g. installed by a manager that skipped customize.sh) so auth is always on.
# BRIDGE_TOKEN gates access -- on Android any local app can reach 127.0.0.1 and
# this server runs as root, so a token is required. The SAME token must be set in
# Miclaw's local MCP config as header  Authorization: Bearer <token>.
TOKEN_DIR=/data/adb/local_mcp_bridge
TOKEN_FILE=$TOKEN_DIR/token
if [ ! -s "$TOKEN_FILE" ]; then
  mkdir -p "$TOKEN_DIR"
  chmod 700 "$TOKEN_DIR"
  TOKEN="$(cat /proc/sys/kernel/random/uuid)$(cat /proc/sys/kernel/random/uuid)"
  echo "$TOKEN" | tr -d '-' > "$TOKEN_FILE"
  chmod 600 "$TOKEN_FILE"
fi
BRIDGE_TOKEN="$(cat "$TOKEN_FILE")"

# Native Android build: uses the OS DNS resolver and CA store, no extra config.
# LOCAL_MCP_ADDR is where it listens (Miclaw connects to http://127.0.0.1:8765/mcp).
BRIDGE_TOKEN="$BRIDGE_TOKEN" \
LOCAL_MCP_ADDR=127.0.0.1:8765 \
  "$MODDIR/local-bridge-android" > /data/local/tmp/local-bridge.log 2>&1 &
