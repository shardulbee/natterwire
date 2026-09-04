#!/bin/sh
set -eu

label="com.shardul.natterwire"
old_label="com.shardul.messages-rest"
domain="gui/$(id -u)"
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
runtime_dir="$HOME/Library/Application Support/natterwire"
old_runtime_dir="$HOME/Library/Application Support/messages-rest"
log_dir="$HOME/Library/Logs/natterwire"
old_log_dir="$HOME/Library/Logs/messages-rest"
agent_dir="$HOME/Library/LaunchAgents"
app="$HOME/Applications/Natterwire.app"
old_app="$HOME/Applications/Messages REST.app"
binary="$app/Contents/MacOS/natterwire"
token_file="$runtime_dir/token"
old_token="$old_runtime_dir/token"
plist="$agent_dir/$label.plist"
old_plist="$agent_dir/$old_label.plist"

"$project_dir/scripts/build-app" "$@" >/dev/null
umask 077
mkdir -p "$runtime_dir" "$log_dir" "$agent_dir" "$HOME/Applications"
chmod 700 "$runtime_dir" "$log_dir"

if [ -L "$token_file" ] || { [ -e "$token_file" ] && [ ! -f "$token_file" ]; }; then
    echo "Refusing unsafe token path: $token_file" >&2
    exit 1
fi
if [ ! -e "$token_file" ]; then
    token_tmp="$runtime_dir/.token.$$"
    trap 'rm -f "$token_tmp"' EXIT HUP INT TERM
    if [ -e "$old_token" ] || [ -L "$old_token" ]; then
        if [ ! -f "$old_token" ] || [ -L "$old_token" ] \
            || [ "$(stat -f %u "$old_token")" != "$(id -u)" ] \
            || [ $((0$(stat -f %Lp "$old_token") & 077)) -ne 0 ]; then
            echo "Refusing unsafe legacy token path: $old_token" >&2
            exit 1
        fi
        cp "$old_token" "$token_tmp"
    else
        /usr/bin/openssl rand -hex 32 > "$token_tmp"
    fi
    chmod 600 "$token_tmp"
    mv "$token_tmp" "$token_file"
    trap - EXIT HUP INT TERM
fi
chmod 600 "$token_file"

launchctl bootout "$domain/$label" 2>/dev/null || true
pkill -x natterwire 2>/dev/null || true
sleep 1
rm -rf "$app"
ditto "$project_dir/dist/Natterwire.app" "$app"
rm -rf "$project_dir/dist/Natterwire.app"
/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister -f "$app" >/dev/null 2>&1 || true

sed \
    -e "s|__PROGRAM__|$binary|g" \
    -e "s|__TOKEN_FILE__|$token_file|g" \
    -e "s|__LOG_DIR__|$log_dir|g" \
    "$project_dir/launchd/$label.plist" > "$plist"
chmod 644 "$plist"
plutil -lint "$plist" >/dev/null

# The replacement files and credential are in place before the old service is stopped.
launchctl bootout "$domain/$old_label" 2>/dev/null || true
if ! launchctl bootstrap "$domain" "$plist"; then
    [ -f "$old_plist" ] && launchctl bootstrap "$domain" "$old_plist" 2>/dev/null || true
    exit 1
fi
launchctl kickstart -k "$domain/$label"
launchctl print "$domain/$label" >/dev/null

# Cleanup happens only after launchd accepts the replacement.
rm -f "$old_plist"
rm -rf "$old_app" "$old_runtime_dir" "$old_log_dir"

echo "Installed and started $label"
echo "App: $app"
echo "Token file: $token_file (contents not displayed)"
echo "LaunchAgent: $plist"
echo "Logs: $log_dir"
