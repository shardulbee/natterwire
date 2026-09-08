# Natterwire API

Loopback-only Go service on port 8741, with an [embedded browser client](web/README.md). Reads Messages through read-only SQLite; sends through Messages.app on macOS. No authentication: restrict Tailscale access to trusted devices and never expose it publicly.

## macOS setup

Requires Go 1.25+ and Xcode command-line tools. Quit Natterwire, then run `scripts/install.sh` and open `~/Applications/Natterwire.app`. The installer reuses the existing signing identity; `--sign IDENTITY` or `--adhoc` overrides it. It removes the legacy LaunchAgent but does not start the app.

The app starts without a window. Use the menu-bar fox → Open Natterwire for status and permission settings, or Quit Natterwire to stop the API. Closing the window leaves the API running.

Grant the installed app Full Disk Access for Messages, Contacts access for names, and Automation access for sending through Messages. Messages access retries automatically after permission changes. Updates may require re-adding the app to Full Disk Access. Do not grant access to a shell or disable TCC.

For remote/browser access, run `tailscale serve --bg 8741` and use its HTTPS URL. Quit before running `scripts/uninstall.sh`; unrecognized pre-Go app backups are retained. Installation details live in [install.sh](../scripts/install.sh) and [build-app](../scripts/build-app).

## Linux fixture workflow

Linux supports fixture reads and fake-sender tests, not native Contacts or sending. From the repository root:

```sh
go -C api build -o bin/natterwire-api .
go -C tui build -o bin/natterwire-tui .
python3 scripts/create-fixture.py /tmp/natterwire-fixture.db
api/bin/natterwire-api --db /tmp/natterwire-fixture.db \
  --contacts api/testdata/contacts.json --pins '' --push-state ''
# In another terminal:
tui/bin/natterwire-tui
```

Use `api/bin/natterwire-api --help` for flags. Explicit flags run the foreground CLI; quit the Mac app first to avoid a port conflict. For live database copies, use SQLite backup rather than copying `chat.db` without its WAL.

## Source and contracts

- [Routes](main.go), [queries and pagination](database.go), [API tests](api_test.go).
- [Sending and idempotency](send.go), [send tests](send_test.go), [Messages adapter](send_darwin.go). Ambiguous failures must retry the same request ID; IDs expire on restart. Never blindly resend after losing an ID.
- [Body decoding](body.go), [decoder tests](body_test.go), [images](image.go), [media tests](media_test.go).
- [Native app](application_darwin.m), [Contacts](contacts_darwin.m).

## Tests

Run the [required Go checks](../AGENTS.md), then optional decoder and end-to-end checks from the repository root:

```sh
go -C api test -run '^$' -fuzz FuzzBody -fuzztime 10s
tui/.venv/bin/python tui/tests/integration.py api/bin/natterwire-api tui/bin/natterwire-tui
```

Build both binaries and install the [TUI test dependencies](../tui/README.md#checks) first. Linux cannot verify macOS permissions, app lifecycle, signing, native Contacts, or live Messages decoding. Mac checks require isolated source/build directories; installation, service restarts, and real message sends require authorization.
