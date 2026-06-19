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

# Backend config (sandbox.conf): points the gateway at your remote sandbox-mcp server.
# Carried forward on update so you don't re-enter it; lives in the module dir so it is
# removed on uninstall.
CONF_FILE="$MODPATH/sandbox.conf"
OLD_CONF="/data/adb/modules/local_mcp_bridge/sandbox.conf"
if [ -s "$OLD_CONF" ]; then
  cp "$OLD_CONF" "$CONF_FILE"
  ui_print "- Carried over the existing sandbox.conf."
else
  cat > "$CONF_FILE" <<'EOF'
# Remote sandbox-mcp server the gateway proxies to. EDIT BOTH, then reboot.
# SANDBOX_TOKEN is your server's SMCP_TOKEN (the same Bearer the MCP uses).
SANDBOX_BASE_URL=https://sandbox.example.com:40443
SANDBOX_TOKEN=
EOF
  ui_print "- Created sandbox.conf template (you MUST edit it, see below)."
fi
chmod 600 "$CONF_FILE"

ui_print " "
ui_print "*********************************************************"
ui_print " 1) Add this header to Miclaw's local MCP server (port 8765):"
ui_print "      Authorization: Bearer $(cat "$TOKEN_FILE")"
ui_print "    Re-read it any time with:"
ui_print "      su -c 'cat /data/adb/modules/local_mcp_bridge/token'"
ui_print " "
ui_print " 2) Point the gateway at your sandbox server, then reboot:"
ui_print "      su -c 'vi /data/adb/modules/local_mcp_bridge/sandbox.conf'"
ui_print "    Set SANDBOX_BASE_URL and SANDBOX_TOKEN. Until both are set,"
ui_print "    Miclaw's connection will (intentionally) fail to load."
ui_print "*********************************************************"
ui_print " "

set_perm "$MODPATH/local-bridge-android" 0 0 0755
