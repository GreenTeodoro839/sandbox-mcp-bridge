#!/system/bin/sh
# (Re)start the sandbox-gateway daemon. Shared by service.sh (boot) and action.sh
# (KernelSU action button), so the env/launch logic lives in ONE place. Idempotent:
# stops any running instance first, then starts detached (new session) so the daemon
# survives the calling shell exiting -- critical for the action.sh path.
MODDIR=${MODDIR:-${0%/*}}
BIN="$MODDIR/local-bridge-android"
LOG=/data/local/tmp/local-bridge.log

chmod 0755 "$BIN" 2>/dev/null

# Bridge token (inbound auth). Normally created at install by customize.sh;
# fallback-generate so auth is always on.
TOKEN_FILE="$MODDIR/token"
if [ ! -s "$TOKEN_FILE" ]; then
  echo "$(cat /proc/sys/kernel/random/uuid)$(cat /proc/sys/kernel/random/uuid)" | tr -d '-' > "$TOKEN_FILE"
  chmod 600 "$TOKEN_FILE"
fi
BRIDGE_TOKEN="$(cat "$TOKEN_FILE")"

# Backend config (SANDBOX_BASE_URL / SANDBOX_TOKEN) -- the remote sandbox-mcp server
# the gateway proxies to. If unset, initialize() returns 503 so Miclaw won't load it.
CONF="$MODDIR/sandbox.conf"
[ -s "$CONF" ] && . "$CONF"

# Stop any running instance, then start fresh.
pkill -f "$BIN" 2>/dev/null
sleep 1

LOCAL_MCP_ADDR=127.0.0.1:8765
export BRIDGE_TOKEN LOCAL_MCP_ADDR SANDBOX_BASE_URL SANDBOX_TOKEN

# Detach into a new session so it outlives this shell (the action runner may reap the
# action's process group on exit). Fall back to nohup if setsid is unavailable.
if command -v setsid >/dev/null 2>&1; then
  setsid "$BIN" > "$LOG" 2>&1 &
else
  nohup "$BIN" > "$LOG" 2>&1 &
fi
