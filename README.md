<p align="center">
  <img src="macos/Natterwire/Assets/NatterwireIcon.png" alt="Natterwire fox switchboard icon" width="180">
</p>

# Natterwire

Natterwire is a native macOS menu-bar app that exposes a read-only REST API for the local Messages database.

The [Linux terminal client](tui/README.md) uses Go and Vaxis to browse chats and compose local drafts. Sending is not supported by the API yet.

## Install

Natterwire requires macOS 14 or newer, Swift 6, and Xcode command-line tools.

```sh
scripts/install.sh
```

The installer builds and signs `~/Applications/Natterwire.app` and starts the per-user `com.shardul.natterwire` LaunchAgent. Grant the app **Full Disk Access** in System Settings, then choose **Retry** from the menu-bar popover. Crashes restart automatically; **Quit** stops the app until the next login or `launchctl kickstart -k gui/$(id -u)/com.shardul.natterwire`.

Uninstall with `scripts/uninstall.sh`. Pass `--purge` to also remove logs.

## API

```text
Local     http://127.0.0.1:8741
Tailnet   https://mac.example.ts.net:8741

GET /chats
GET /chats/:identifier/messages
GET /messages/:identifier
```

Lists return `{ "items": [...], "nextBefore": "..." }`; `limit` defaults to 50 when absent and accepts integers from 1 through 100, while `before` accepts the prior cursor. Chats follow Messages ordering: saved pins first in pin order, then unpinned chats by newest activity. Chat identifiers are URL-safe opaque encodings of Messages chat GUIDs. Natterwire uses Contacts names for direct chats and unnamed group participants when permission is available, otherwise it returns their raw handles. An unnamed group with no participants returns `Group chat`. Valid message rows whose body cannot be decoded return `text: ""` so pagination remains stable.

```sh
curl 'http://127.0.0.1:8741/chats?limit=20'
```

## Security

Natterwire opens `chat.db` read-only and never sends or modifies Messages data. It binds only to loopback; local processes can access the API. Tailscale Serve provides remote tailnet access, so restrict TCP 8741 to intended devices in the tailnet policy. Never use a LAN bind or Tailscale Funnel.
