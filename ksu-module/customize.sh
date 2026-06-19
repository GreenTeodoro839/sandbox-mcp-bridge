#!/system/bin/sh
# Runs once at install time (sourced by the Magisk/KernelSU module installer).
# Generates a random bridge token on first install and prints it so you can add
# it to Miclaw's local MCP server config. The token is stored OUTSIDE the module
# dir so it survives module updates/reinstalls.

TOKEN_DIR=/data/adb/local_mcp_bridge
TOKEN_FILE=$TOKEN_DIR/token

mkdir -p "$TOKEN_DIR"
chmod 700 "$TOKEN_DIR"

if [ ! -s "$TOKEN_FILE" ]; then
  # 2x kernel UUID (always present on Android) -> 64 hex chars, no extra tools.
  TOKEN="$(cat /proc/sys/kernel/random/uuid)$(cat /proc/sys/kernel/random/uuid)"
  TOKEN="$(echo "$TOKEN" | tr -d '-')"
  echo "$TOKEN" > "$TOKEN_FILE"
  chmod 600 "$TOKEN_FILE"
  ui_print "- Generated a new bridge token."
else
  ui_print "- Existing bridge token kept."
fi

ui_print " "
ui_print "*********************************************************"
ui_print " Add this header to Miclaw's local MCP server (port 8765):"
ui_print "   Authorization: Bearer $(cat "$TOKEN_FILE")"
ui_print " "
ui_print " You can re-read it any time with:"
ui_print "   su -c 'cat $TOKEN_FILE'"
ui_print "*********************************************************"
ui_print " "

set_perm "$MODPATH/local-bridge-android" 0 0 0755
