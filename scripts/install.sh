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
CGO_ENABLED=0 go -C "$root/tui" build -trimpath -o "$stage/natterwire-tui" .

if [[ "$(uname -s)" == Darwin ]]; then
  apps="$HOME/Applications"
  installed="$apps/Natterwire.app"
  built="$stage/Natterwire.app"
  build_args=(--output "$built" --reference "$installed")
  [[ -z "$identity" ]] || build_args+=(--sign "$identity")
  "$root/scripts/build-app" "${build_args[@]}"

  mkdir -p "$apps"
  known=false
  if [[ -f "$installed/Contents/Info.plist" && -x "$installed/Contents/MacOS/natterwire-api" ]]; then
    [[ "$(plutil -extract CFBundleIdentifier raw -o - "$installed/Contents/Info.plist" 2>/dev/null || true)" == com.shardul.natterwire.api ]] && known=true
  fi
  if [[ -e "$installed" && "$known" == false && ! -e "$apps/Natterwire.app.pre-go" ]]; then
    mv "$installed" "$apps/Natterwire.app.pre-go"
    echo "Backed up the previous app to $apps/Natterwire.app.pre-go"
  elif [[ -e "$installed" && "$known" == false ]]; then
    echo "Refusing to replace an unrecognized $installed: backup already exists" >&2
    exit 1
  elif [[ -e "$installed" ]]; then
    rm -rf "$stage/previous.app"
    mv "$installed" "$stage/previous.app"
  fi
  mv "$built" "$installed"
  ln -sfn "$installed/Contents/MacOS/natterwire-api" "$bin/natterwire-api"
else
  CGO_ENABLED=0 go -C "$root/api" build -trimpath -o "$stage/natterwire-api" .
  mv -f "$stage/natterwire-api" "$bin/natterwire-api"
fi

mv -f "$stage/natterwire-tui" "$bin/natterwire-tui"

if [[ "$(uname -s)" == Darwin ]]; then
  label=com.shardul.natterwire
  domain="gui/$(id -u)"
  plist="$HOME/Library/LaunchAgents/$label.plist"
  # Retire installations from versions that ran the API through launchd.
  launchctl bootout "$domain/$label" 2>/dev/null || true
  rm -f "$plist"
  echo "Installed Natterwire API and $bin/natterwire-tui"
  echo "Open $installed to start Natterwire."
else
  echo "Installed Natterwire API and $bin/natterwire-tui"
fi
