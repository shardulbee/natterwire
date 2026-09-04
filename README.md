<p align="center">
  <img src="macos/Natterwire/Assets/NatterwireIcon.png" alt="Natterwire fox switchboard icon" width="180">
</p>

# Natterwire

Natterwire is a native macOS menu-bar app that exposes an authenticated, read-only REST API for the local Messages database.

## Install

Natterwire requires macOS 14 or newer, Swift 6, and Xcode command-line tools.

```sh
scripts/install.sh
```

The installer builds and signs `~/Applications/Natterwire.app`, creates an owner-only token under `~/Library/Application Support/natterwire`, and starts the per-user `com.shardul.natterwire` LaunchAgent. Grant the app **Full Disk Access** in System Settings, then choose **Retry** from the menu-bar popover. Crashes restart automatically; **Quit** stops the app until the next login or `launchctl kickstart -k gui/$(id -u)/com.shardul.natterwire`.

Uninstall with `scripts/uninstall.sh`. Pass `--purge` to also remove the token and logs.

## API

```text
Local     http://127.0.0.1:8741
Tailnet   https://mac.example.ts.net:8741

GET /chats
GET /chats/:identifier/messages
GET /messages/:identifier
```

Every request requires `Authorization: Bearer <token>`. Lists return `{ "items": [...], "nextBefore": "..." }`; `limit` defaults to 50 when absent and accepts integers from 1 through 100, while `before` accepts the prior cursor. Chat identifiers are URL-safe opaque encodings of Messages chat GUIDs. Valid message rows whose body cannot be decoded return `text: ""` so pagination remains stable.

```sh
TOKEN=$(cat "$HOME/Library/Application Support/natterwire/token")
curl -H "Authorization: Bearer $TOKEN" \
  'http://127.0.0.1:8741/chats?limit=20'
```

## Security

Natterwire opens `chat.db` read-only and never sends or modifies Messages data. It binds only to loopback; Tailscale Serve provides tailnet-only access, with no LAN listener or Funnel. Keep the bearer token private. `NATTERWIRE_API_TOKEN` (then legacy `MESSAGES_API_TOKEN`) overrides `NATTERWIRE_API_TOKEN_FILE` (then legacy `MESSAGES_API_TOKEN_FILE`); otherwise the app securely reads its owner-only default token file.
