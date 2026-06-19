#!/system/bin/sh
# Start the sandbox-gateway MCP once the system has finished booting. The actual
# env setup + launch lives in launch.sh (shared with action.sh's restart button).
until [ "$(getprop sys.boot_completed)" = 1 ]; do
  sleep 2
done

MODDIR=${0%/*}
MODDIR="$MODDIR" sh "$MODDIR/launch.sh"
