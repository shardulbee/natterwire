#!/bin/sh
set -eu

if [ "$(uname -s)" = Darwin ]; then
    launchctl bootout "gui/$(id -u)/com.shardul.natterwire" 2>/dev/null || true
    rm -f "$HOME/Library/LaunchAgents/com.shardul.natterwire.plist"
fi
rm -f "$HOME/.local/bin/natterwire-api" "$HOME/.local/bin/natterwire-tui"
app="$HOME/Applications/Natterwire.app"
if [ "$(uname -s)" = Darwin ] && [ -f "$app/Contents/Info.plist" ] && [ -x "$app/Contents/MacOS/natterwire-api" ] && \
   [ "$(plutil -extract CFBundleIdentifier raw -o - "$app/Contents/Info.plist" 2>/dev/null || true)" = "com.shardul.natterwire.api" ]; then
    rm -rf "$app"
fi
if [ "${1:-}" = "--purge" ]; then
    rm -f "$HOME/Library/Logs/natterwire/stdout.log" "$HOME/Library/Logs/natterwire/stderr.log"
    rmdir "$HOME/Library/Logs/natterwire" 2>/dev/null || true
fi
echo "Removed the Go app, command links, and LaunchAgent. Any pre-Go app backup was retained."
