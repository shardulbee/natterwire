<p align="center">
  <img src="macos/Natterwire/Assets/NatterwireIcon.png" alt="Natterwire fox switchboard icon" width="180">
</p>

# Natterwire

Natterwire is a Messages API and terminal client, both written in Go. On macOS the API is packaged as `Natterwire.app` with a fox menu for opening its status and permissions window or quitting, and no Dock icon. The API reads the live Messages database on macOS and SQLite copies or synthetic fixtures on Linux.

The [terminal client](tui/README.md) browses chats and sends text to existing conversations through Messages.app after [send-token setup](api/README.md#sending).

## Install

Requires Go 1.25+. macOS builds also need Xcode command-line tools for the Objective-C Contacts bridge.

```sh
scripts/install.sh
```

On macOS this installs `~/Applications/Natterwire.app`, links `~/.local/bin/natterwire-api` to its sole executable, and installs the TUI. Open the app from Finder to start the API; quitting the app stops it, and reopening it starts it again. The window guides setup for Full Disk Access and Contacts, and retries the Messages database automatically while access is unavailable. See [API setup, migration, and Linux fixtures](api/README.md). Linux installs standalone Go binaries and does not load Contacts by default.

Quit Natterwire before installing an update or uninstalling. Uninstall with `scripts/uninstall.sh`; `--purge` also removes legacy logs.

## API

```text
Local     http://127.0.0.1:8741
Tailnet   https://mac.example.ts.net:8741

GET /chats
GET /chats/:identifier/messages
GET /messages/:identifier
```

Lists return `{ "items": [...], "nextBefore": "..." }`, with `nextBefore: null` at the end. `limit` defaults to 50 and accepts integers from 1 through 100; `before` accepts the prior cursor. See [the API contract](api/README.md#api-contract) for ordering, naming, and archived-body decoding.

Messages include `attachments: [{ id, filename, mimeType, dataBase64 }]`, or `[]` for text-only messages. `dataBase64` contains the original file bytes as standard base64, omitted when the file is unavailable or larger than 10 MiB. Optional metadata is omitted when unknown; local paths are not returned. Attachment-only messages are included even without a text body. Base64 adds roughly 33% to file size, so use smaller pages for media-heavy chats.

HEIC/HEIF images up to 32 megapixels also include `displayDataBase64`, a full-resolution, orientation-correct JPEG for clients without HEIC decoders. The original `dataBase64` and MIME type remain unchanged.

```sh
curl 'http://127.0.0.1:8741/chats?limit=20'
```

## Security

Natterwire opens `chat.db` read-only. Optional text sending uses AppleScript and requires a separate bearer token; reads remain unauthenticated. It binds only to loopback; local processes can access the read API. Tailscale Serve provides remote tailnet access, so restrict TCP 8741 to intended devices in the tailnet policy. Never use a LAN bind or Tailscale Funnel.
