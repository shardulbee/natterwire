#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
bin="$HOME/.local/bin"
identity=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --sign) identity="${2:?--sign requires an identity}"; shift 2 ;;
    --adhoc) identity="-"; shift ;;
    *) echo "Usage: scripts/install.sh [--sign IDENTITY | --adhoc]" >&2; exit 2 ;;
  esac
done
umask 077
mkdir -p "$bin"
stage="$(mktemp -d "$bin/.natterwire.XXXXXX")"
trap 'rm -rf "$stage"' EXIT
CGO_ENABLED=0 go -C "$root/api" build -trimpath -o "$stage/natterwire-api" .
CGO_ENABLED=0 go -C "$root/tui" build -trimpath -o "$stage/natterwire-tui" .

if [[ "$(uname -s)" == Darwin ]]; then
  # Reuse a real signing identity when available, but never assume its TCC grants
  # carry from the former app bundle to this standalone executable.
  if [[ -z "$identity" ]]; then
    installed="$bin/natterwire-api"
    [[ -f "$installed" ]] || installed="$HOME/Applications/Natterwire.app"
    prior="$(codesign -dvv "$installed" 2>&1 | awk -F= '/^Authority=/{print substr($0,index($0,"=")+1); exit}' || true)"
    available="$(security find-identity -v -p codesigning 2>/dev/null || true)"
    if [[ -n "$prior" && "$available" == *"\"$prior\""* ]]; then
      identity="$prior"
    else
      identity="$(printf '%s\n' "$available" | awk -F'"' '/Apple Development:/ {print $2; exit}')"
      identity="${identity:--}"
    fi
  fi
  codesign --force --sign "$identity" --identifier com.shardul.natterwire.api --timestamp=none "$stage/natterwire-api"
  codesign --verify --strict "$stage/natterwire-api"
fi

mv -f "$stage/natterwire-api" "$bin/natterwire-api"
mv -f "$stage/natterwire-tui" "$bin/natterwire-tui"

if [[ "$(uname -s)" == Darwin ]]; then
  label=com.shardul.natterwire
  domain="gui/$(id -u)"
  logs="$HOME/Library/Logs/natterwire"
  plist="$HOME/Library/LaunchAgents/$label.plist"
  mkdir -p "$logs" "$(dirname "$plist")"
  chmod 700 "$logs"
  # plutil escapes paths as plist strings, including spaces and XML characters.
  cp "$root/launchd/$label.plist" "$stage/agent.plist"
  plutil -replace ProgramArguments.0 -string "$bin/natterwire-api" "$stage/agent.plist"
  plutil -replace StandardOutPath -string "$logs/stdout.log" "$stage/agent.plist"
  plutil -replace StandardErrorPath -string "$logs/stderr.log" "$stage/agent.plist"
  plutil -lint "$stage/agent.plist" >/dev/null
  launchctl bootout "$domain/$label" 2>/dev/null || true
  mv -f "$stage/agent.plist" "$plist"
  launchctl bootstrap "$domain" "$plist"
  launchctl print "$domain/$label" >/dev/null
  echo "LaunchAgent registered. Check $logs/stderr.log and the API before assuming it is running."
  echo "Grant Full Disk Access to $bin/natterwire-api, then restart the LaunchAgent."
  echo "The old Swift app's permission does not transfer. See api/README.md."
fi
echo "Installed $bin/natterwire-api and $bin/natterwire-tui"
