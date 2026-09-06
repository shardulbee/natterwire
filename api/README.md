# Natterwire API

Go HTTP service using pure-Go SQLite. The macOS build uses cgo for its native Contacts bridge; Linux remains a `CGO_ENABLED=0` build with no Contacts integration. The [TUI](../tui/README.md) uses the same API on both platforms.

## Build and run

From the repository root:

```sh
go -C api build -o bin/natterwire-api .
go -C tui build -o bin/natterwire-tui .
api/bin/natterwire-api --help
api/bin/natterwire-api
```

The service binds only to `127.0.0.1`, port 8741 by default. `--port` selects another port, not another interface. `--db` overrides `NATTERWIRE_DB_PATH`, then `MESSAGES_DB_PATH`, then `~/Library/Messages/chat.db`. It opens SQLite with `mode=ro` and `query_only`, not `immutable`, so live WAL updates remain visible. Missing databases are not created. Copies of live databases must include a consistent WAL snapshot; use SQLite's backup facility rather than copying only `chat.db` while Messages is running.

## Linux fixture workflow

Python 3 is needed only to create the fixture. Use the real API, not TUI demo mode:

```sh
python3 scripts/create-fixture.py /tmp/natterwire-fixture.db
api/bin/natterwire-api --db /tmp/natterwire-fixture.db \
  --contacts api/testdata/contacts.json --pins ''
# In another terminal:
tui/bin/natterwire-tui
```

The fixture refuses to overwrite an existing file. It includes archived text, duplicate timestamps, hidden system rows, groups, and an attachment with unavailable bytes. The test suite also creates temporary attachment files and XML/binary pinning plists.

## macOS installation and migration

Run `scripts/install.sh` from an interactive terminal. It builds and signs `~/Applications/Natterwire.app`, links `~/.local/bin/natterwire-api` to the app's only executable, and installs the TUI. It also stops and removes the LaunchAgent left by older releases; it does not register or start anything. The installer tries to reuse the prior Apple Development identity; `--sign IDENTITY` selects one and `--adhoc` uses ad-hoc signing. An unrecognized old app is moved once to `~/Applications/Natterwire.app.pre-go`, never silently deleted.

Quit Natterwire before installing or updating so it releases port 8741, then open `Natterwire.app` from Finder. Quit the app to stop the API and reopen it to start again. Do not open the retained backup while Natterwire is running.

The app lives in the menu bar with a monochrome fox icon and no Dock icon. Its window shows separate access checkmarks for Messages and Contacts and stays open until you dismiss it. Closing the window keeps the API running. Click the menu bar fox and choose Open Natterwire to reopen the window, or Quit Natterwire to stop the app and API. ⌘Q also quits while the window is focused.

The app guides you to System Settings when Full Disk Access is missing and requests Contacts access. Add `~/Applications/Natterwire.app` under Privacy & Security → Full Disk Access. The app retries the Messages database every two seconds, so leave it open after granting access. If macOS itself asks you to Quit & Reopen, use that normal app action; no service commands are needed. Declining Contacts access only leaves names unresolved.

Running `natterwire-api` with explicit command-line flags remains a foreground CLI workflow. Quit the app first to avoid a port conflict.

Full Disk Access is tied to macOS TCC's executable identity and may not transfer to an updated app even when the signing certificate is reused. Re-add the installed app if necessary. Do not grant blanket access to a shell or disable TCC. Quit Natterwire before running `scripts/uninstall.sh`; it removes only a recognized app and command links while retaining the pre-Go backup. `--purge` also removes legacy logs. The installer does not change Tailscale or privacy settings.

## Contacts and pins

On macOS, the native Contacts bridge requests permission on first use and updates the in-memory name map when Contacts changes. No contact export is written. An explicit `--contacts PATH` overrides native Contacts with a JSON dictionary; `--contacts ''` disables names. Linux has no native default, but accepts the same explicit JSON file. Emails are case-insensitive and phone matching uses normalized digits with a last-ten-digit fallback.

Native access requires the app bundle's Contacts usage description. Bare command-line builds fall back to raw handles. Declining Contacts permission does not stop the API; granting or revoking access updates names on subsequent lookups. Contacts permission is requested at launch, independently of Messages access.

Pins load once from `~/Library/Preferences/com.apple.messages.pinning.plist`, using `pD.pP`. Both binary and XML plists work. `--pins PATH` overrides it; `--pins ''` disables it. Missing or inaccessible default preferences fall back to activity order. Restart after changing pins.

## API contract

- `GET /chats`, `GET /chats/:identifier/messages`, and alias `GET /messages/:identifier` retain the former JSON fields and URL-safe base64 chat IDs.
- Lists return `items` and nullable `nextBefore`. Limits are 1–100, default 50. Ranked `v2` chat cursors preserve saved pin order, then newest activity and descending row ID. Legacy recency cursors still work. Message cursors use date and row ID, newest first.
- Reactions, group actions, system items, deleted messages, and rows without a date/content are excluded when the schema supplies those fields. Archived/deleted chats are omitted from the chat list. As before, a known identifier can still query their messages.
- Contact names override direct-chat labels. Named groups retain their label; unnamed groups join deduplicated participant names or handles in database order. Empty groups return `Group chat`.
- Plain `text`, including an empty string, takes precedence over `attributedBody`. The Go decoder reads typedstream v4 NSString and mutable/immutable NSAttributedString backing text, with class references, both endiannesses, and 1/2/4-byte lengths. It does not decode formatting attributes or keyed archives. Unsupported or malformed bodies retain their row with `text: ""`, as before. This is a prefix decoder, not a validator for the trailing attribute graph. Synthetic tests are not proof of coverage for every Apple archive variant; live macOS comparison remains necessary.
- Attachments retain IDs, optional filenames/MIME types, and base64 original bytes up to 10 MiB. Unavailable or oversized files omit `dataBase64`; local paths are not exposed. Text-only messages return `attachments: []`.
- `media=metadata` omits inline bytes and supplies opaque `mediaID`, file `version`, and oriented image dimensions. `GET /attachments/:mediaID?version=...` returns display bytes, with a 32 MiB limit and 128 MiB LRU cache. Missing images return 404, stale or omitted versions 409, oversized images 413. At most two media requests run concurrently; excess requests return 503 without blocking metadata requests.
- HEIC display JPEGs use `displayDataBase64`, preserving full resolution and HEIF rotation/mirroring. The embedded WASM decoder runs on Linux and macOS without a native library. Images over 32 megapixels or unsupported images omit the display copy. EXIF-only orientation without HEIF transform properties is not applied; live Apple image parity remains unverified.
- JPEG EXIF orientation is applied to binary display images and advertised dimensions. Original inline bytes remain unchanged.

## Tests

```sh
go -C api test -race ./...
go -C api vet ./...
go -C api test -run '^$' -fuzz FuzzBody -fuzztime 10s
go -C tui test -race ./...
go -C tui vet ./...
tui/.venv/bin/python tui/tests/integration.py api/bin/natterwire-api tui/bin/natterwire-tui
```

Build both binaries first and install the [TUI test dependencies](../tui/README.md#checks) for the integration check. It runs the real service and TUI in a PTY and checks archived text, naming, drafts, live SQLite WAL updates, and shutdown. Linux tests cannot verify macOS TCC, the app lifecycle, code signing, the Contacts adapter, or decoding parity against a live Messages database.
