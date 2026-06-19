#!/system/bin/sh
# KernelSU "action" button: one-tap restart of the sandbox-gateway MCP. Output below
# is streamed to the manager's action console.
MODDIR=${0%/*}

echo "Restarting sandbox-gateway MCP..."
MODDIR="$MODDIR" sh "$MODDIR/launch.sh"
sleep 1

PIDS="$(pgrep -f "$MODDIR/local-bridge-android" 2>/dev/null | tr '\n' ' ')"
if [ -n "$PIDS" ]; then
  echo "OK: running (pid $PIDS) on http://127.0.0.1:8765/mcp"
else
  echo "FAILED to start -- last log lines:"
  tail -n 20 /data/local/tmp/local-bridge.log 2>/dev/null
fi

[ -s "$MODDIR/sandbox.conf" ] || echo "NOTE: sandbox.conf is empty -- set SANDBOX_BASE_URL/SANDBOX_TOKEN or Miclaw won't load (503)."
echo "Miclaw header: Authorization: Bearer $(cat "$MODDIR/token" 2>/dev/null)"
