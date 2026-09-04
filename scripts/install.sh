#!/bin/sh
set -eu

label="com.shardul.natterwire"
old_label="com.shardul.messages-rest"
domain="gui/$(id -u)"
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
log_dir="$HOME/Library/Logs/natterwire"
old_log_dir="$HOME/Library/Logs/messages-rest"
agent_dir="$HOME/Library/LaunchAgents"
app="$HOME/Applications/Natterwire.app"
old_app="$HOME/Applications/Messages REST.app"
binary="$app/Contents/MacOS/natterwire"
plist="$agent_dir/$label.plist"
old_plist="$agent_dir/$old_label.plist"

"$project_dir/scripts/build-app" "$@" >/dev/null
umask 077
mkdir -p "$log_dir" "$agent_dir" "$HOME/Applications"
chmod 700 "$log_dir"

launchctl bootout "$domain/$label" 2>/dev/null || true
pkill -x natterwire 2>/dev/null || true
sleep 1
rm -rf "$app"
ditto "$project_dir/dist/Natterwire.app" "$app"
rm -rf "$project_dir/dist/Natterwire.app"
/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -f "$app" >/dev/null 2>&1 || true

sed \
    -e "s|__PROGRAM__|$binary|g" \
    -e "s|__LOG_DIR__|$log_dir|g" \
    "$project_dir/launchd/$label.plist" > "$plist"
chmod 644 "$plist"
plutil -lint "$plist" >/dev/null

# The replacement files are in place before the old service is stopped.
launchctl bootout "$domain/$old_label" 2>/dev/null || true
if ! launchctl bootstrap "$domain" "$plist"; then
    [ -f "$old_plist" ] && launchctl bootstrap "$domain" "$old_plist" 2>/dev/null || true
    exit 1
fi
launchctl kickstart -k "$domain/$label"
launchctl print "$domain/$label" >/dev/null

# Cleanup happens only after launchd accepts the replacement.
rm -f "$old_plist"
rm -rf "$old_app" "$old_log_dir"

echo "Installed and started $label"
echo "App: $app"
echo "LaunchAgent: $plist"
echo "Logs: $log_dir"
