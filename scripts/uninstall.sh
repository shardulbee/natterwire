#!/bin/sh
set -eu

label="com.shardul.natterwire"
domain="gui/$(id -u)"
log_dir="$HOME/Library/Logs/natterwire"
plist="$HOME/Library/LaunchAgents/$label.plist"
app="$HOME/Applications/Natterwire.app"

launchctl bootout "$domain/$label" 2>/dev/null || true
rm -f "$plist"
rm -rf "$app"

if [ "${1:-}" = "--purge" ]; then
    rm -f "$log_dir/stdout.log" "$log_dir/stderr.log"
    rmdir "$log_dir" 2>/dev/null || true
    echo "Uninstalled $label and removed its app and logs"
else
    echo "Uninstalled $label and its app; retained logs (use --purge to remove them)"
fi
