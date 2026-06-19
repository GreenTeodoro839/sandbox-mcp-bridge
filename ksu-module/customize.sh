#!/system/bin/sh
# Runs once at install time (sourced by the Magisk/KernelSU module installer).
# The bridge token lives INSIDE the module dir ($MODPATH/token), so it is removed
# cleanly when the module is uninstalled. On an update the module dir is replaced,
# so we carry the existing token forward from the currently-installed module --
# you keep the same token (no need to re-paste into Miclaw), yet nothing is left
# behind after uninstall.

TOKEN_FILE="$MODPATH/token"
OLD_TOKEN="/data/adb/modules/local_mcp_bridge/token"

if [ -s "$OLD_TOKEN" ]; then
  cp "$OLD_TOKEN" "$TOKEN_FILE"
  ui_print "- Carried over the existing bridge token."
else
  # 2x kernel UUID (always present on Android) -> 64 hex chars, no extra tools.
  TOKEN="$(cat /proc/sys/kernel/random/uuid)$(cat /proc/sys/kernel/random/uuid)"
  echo "$TOKEN" | tr -d '-' > "$TOKEN_FILE"
  ui_print "- Generated a new bridge token."
fi
chmod 600 "$TOKEN_FILE"

ui_print " "
ui_print "*********************************************************"
ui_print " Add this header to Miclaw's local MCP server (port 8765):"
ui_print "   Authorization: Bearer $(cat "$TOKEN_FILE")"
ui_print " "
ui_print " Re-read it any time with:"
ui_print "   su -c 'cat /data/adb/modules/local_mcp_bridge/token'"
ui_print "*********************************************************"
ui_print " "

set_perm "$MODPATH/local-bridge-android" 0 0 0755
