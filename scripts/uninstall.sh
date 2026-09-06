#!/bin/sh
set -eu

if [ "$(uname -s)" = Darwin ]; then
    launchctl bootout "gui/$(id -u)/com.shardul.natterwire" 2>/dev/null || true
    rm -f "$HOME/Library/LaunchAgents/com.shardul.natterwire.plist"
fi
rm -f "$HOME/.local/bin/natterwire-api" "$HOME/.local/bin/natterwire-tui"
if [ "${1:-}" = "--purge" ]; then
    rm -f "$HOME/Library/Logs/natterwire/stdout.log" "$HOME/Library/Logs/natterwire/stderr.log"
    rmdir "$HOME/Library/Logs/natterwire" 2>/dev/null || true
fi
echo "Removed Go binaries and LaunchAgent. Contacts exports and any old Natterwire.app were retained."
