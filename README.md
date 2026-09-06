<p align="center">
  <img src="macos/Natterwire/Assets/NatterwireIcon.png" alt="Natterwire fox switchboard icon" width="180">
</p>

# Natterwire

Natterwire is a read-only Messages API and terminal client, both written in Go. On macOS the API is packaged as a background-only `Natterwire.app`; it has no window or menu. The API reads the live Messages database on macOS and SQLite copies or synthetic fixtures on Linux.

The [terminal client](tui/README.md) browses chats and composes local drafts. Sending is not supported.

## Install

Requires Go 1.25+. macOS builds also need Xcode command-line tools for the Objective-C Contacts bridge.

```sh
scripts/install.sh
```

On macOS this installs `~/Applications/Natterwire.app`, links `~/.local/bin/natterwire-api` to its sole executable, installs the TUI, and registers the API LaunchAgent. The app requests Contacts access on first use and keeps names updated in memory. Full Disk Access is still required for Messages and cannot be granted automatically. See [API setup, migration, and Linux fixtures](api/README.md). Linux installs standalone Go binaries and does not load Contacts by default.

Uninstall with `scripts/uninstall.sh`. Pass `--purge` to also remove logs.

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

Natterwire opens `chat.db` read-only and never sends or modifies Messages data. It binds only to loopback; local processes can access the API. Tailscale Serve provides remote tailnet access, so restrict TCP 8741 to intended devices in the tailnet policy. Never use a LAN bind or Tailscale Funnel.
