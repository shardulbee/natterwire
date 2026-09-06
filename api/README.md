# Natterwire API

Standalone Go HTTP service using pure-Go SQLite. No C compiler or Apple framework is required to build or run it. The [TUI](../tui/README.md) uses the same API on Linux and macOS.

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

Run `scripts/install.sh` from an interactive terminal. It builds both Go binaries in `~/.local/bin`, signs the API, and replaces the per-user `com.shardul.natterwire` LaunchAgent. It tries to reuse an available signing identity from the prior installation. `--sign IDENTITY` selects one explicitly; `--adhoc` uses ad-hoc signing. Ad-hoc rebuilds can require granting access again. No Xcode/Swift build is involved, but macOS signing tools must be available.

Quit a manually launched old menu-bar app before installing so it releases port 8741. The installer stops the old LaunchAgent, but retains `~/Applications/Natterwire.app` for rollback. Do not reopen it while the Go service is running. Once the Go API works, the old app can be removed manually.

In System Settings → Privacy & Security → Full Disk Access, add the installed `~/.local/bin/natterwire-api`. Use Shift+Command+G in the file picker to enter the hidden directory. Restart the LaunchAgent after changing access:

```sh
launchctl kickstart -k gui/$(id -u)/com.shardul.natterwire
launchctl print gui/$(id -u)/com.shardul.natterwire
tail -n 20 ~/Library/Logs/natterwire/stderr.log
curl -fsS 'http://127.0.0.1:8741/chats?limit=1' >/dev/null
```

A registered LaunchAgent is not proof that it can read Messages. Check the HTTP response and logs. Full Disk Access is tied to macOS TCC's executable identity and responsible launcher. A successful run from a terminal granted Full Disk Access does not prove a LaunchAgent has access. The old Swift app's grant does not transfer, even if the signing certificate is reused. If direct execution is denied, grant the terminal application access and restart it. If only launchd fails, re-add the installed binary and verify from launchd again. Do not grant blanket access to a shell or disable TCC to work around this.

`launchctl bootout gui/$(id -u)/com.shardul.natterwire` stops the service until the next login or bootstrap. Crashes retry after launchd's throttle interval. `scripts/uninstall.sh` removes the Go binaries and LaunchAgent; `--purge` also removes logs. Contacts exports and the old app remain untouched. The installer does not change Tailscale configuration.

## Contacts and pins

The API loads a handle-to-name JSON dictionary once at startup, by default `~/.config/natterwire/contacts.json`. A missing default file means raw handles. Explicit `--contacts PATH` errors fail startup; `--contacts ''` disables names. Emails are case-insensitive, phones use normalized digits and the previous last-ten-digit fallback. Ambiguous phone suffixes still use one matching contact, so full international numbers are preferable.

On macOS, export Contacts interactively with the small JXA adapter:

```sh
umask 077
mkdir -p ~/.config/natterwire
osascript -l JavaScript scripts/export-contacts.js > ~/.config/natterwire/contacts.json.tmp &&
  mv ~/.config/natterwire/contacts.json.tmp ~/.config/natterwire/contacts.json
```

macOS may request permission for the terminal to automate Contacts. This is separate from Messages Full Disk Access. The export contains private names, emails, and phone numbers; do not commit or upload it. Restart the API to load it. Re-export after changing contacts. Linux tests use fake dictionaries with the same shape. Unlike the old app, the service does not request Contacts permission or fetch Contacts itself.

Pins load once from `~/Library/Preferences/com.apple.messages.pinning.plist`, using `pD.pP`. Both binary and XML plists work. `--pins PATH` overrides it; `--pins ''` disables it. Missing or inaccessible default preferences fall back to activity order. Restart after changing pins.

## API contract

- `GET /chats`, `GET /chats/:identifier/messages`, and alias `GET /messages/:identifier` retain the former JSON fields and URL-safe base64 chat IDs.
- Lists return `items` and nullable `nextBefore`. Limits are 1–100, default 50. Ranked `v2` chat cursors preserve saved pin order, then newest activity and descending row ID. Legacy recency cursors still work. Message cursors use date and row ID, newest first.
- Reactions, group actions, system items, deleted messages, and rows without a date/content are excluded when the schema supplies those fields. Archived/deleted chats are omitted from the chat list. As before, a known identifier can still query their messages.
- Contact names override direct-chat labels. Named groups retain their label; unnamed groups join deduplicated participant names or handles in database order. Empty groups return `Group chat`.
- Plain `text`, including an empty string, takes precedence over `attributedBody`. The Go decoder reads typedstream v4 NSString and mutable/immutable NSAttributedString backing text, with class references, both endiannesses, and 1/2/4-byte lengths. It does not decode formatting attributes or keyed archives. Unsupported or malformed bodies retain their row with `text: ""`, as before. This is a prefix decoder, not a validator for the trailing attribute graph. Synthetic tests are not proof of coverage for every Apple archive variant; live macOS comparison remains necessary.
- Attachments retain IDs, optional filenames/MIME types, and base64 original bytes up to 10 MiB. Unavailable or oversized files omit `dataBase64`; local paths are not exposed. Text-only messages return `attachments: []`.
- HEIC display JPEGs use `displayDataBase64`, preserving full resolution and HEIF rotation/mirroring. The embedded WASM decoder runs on Linux and macOS without a native library. Images over 32 megapixels or unsupported images omit the display copy. EXIF-only orientation without HEIF transform properties is not applied; live Apple image parity remains unverified.

## Tests

```sh
go -C api test -race ./...
go -C api vet ./...
go -C api test -run '^$' -fuzz FuzzBody -fuzztime 10s
go -C tui test -race ./...
go -C tui vet ./...
tui/.venv/bin/python tui/tests/integration.py api/bin/natterwire-api tui/bin/natterwire-tui
```

Build both binaries first and install the [TUI test dependencies](../tui/README.md#checks) for the integration check. It runs the real service and TUI in a PTY and checks archived text, naming, drafts, live SQLite WAL updates, and shutdown. Linux tests cannot verify macOS TCC, launchd, code signing, the Contacts adapter, or decoding parity against a live Messages database.
