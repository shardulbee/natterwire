# Natterwire TUI

Linux/macOS client built with Go 1.25+ and [Go Vaxis](https://github.com/rockorager/vaxis), pinned in `go.mod`. One binary, no `curl` or native library dependency. The Mac API remains read-only: drafts cannot be sent and disappear when you quit.

## Run

From this directory:

```sh
go build -o bin/natterwire-tui .
./bin/natterwire-tui --demo
./bin/natterwire-tui --url https://mac.example.ts.net:8741
```

`NATTERWIRE_URL` sets the default URL; otherwise the client uses `http://127.0.0.1:8741`. Connect Linux to the same tailnet as the Mac. Install the binary anywhere on `PATH`, for example `~/.local/bin/natterwire-tui`.

To cross-compile an x86-64 Linux binary from a Mac:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o bin/linux-amd64/natterwire-tui .
```

Use `GOARCH=arm64` and a different output directory for ARM Linux.

## Keys

| Focus | Keys |
| --- | --- |
| Sidebar | `j/k` switch chats, `l` or Enter focuses transcript, `i` opens its draft, `n` loads more chats |
| Transcript | `j/k` scroll lines, `d/u` half-pages, `h` sidebar, `i` draft, `o` older history, `G` latest |
| Insert | Type normally, Esc returns to transcript; Enter explains why sending is unavailable |
| Outside insert | `r` refreshes chats and open transcript, `q` quits |
| Anywhere | Ctrl+L repaints, Ctrl+C quits |

Requires at least 60 columns by 14 rows. Arrow keys also work. Color indicates focus; `>` marks the open chat. Selecting a chat displays its cached transcript immediately while refreshing in the background; uncached chats need an initial fetch. Switching chats resets to the bottom; drafts survive switches within the running session. The empty composer stays hidden until insert mode.

Image attachments render inline as colored half-block previews, up to 48 columns by 12 rows, and scroll with the transcript. PNG, JPEG, GIF (first frame), and WebP are supported; HEIC and other unsupported, missing, or oversized images show a filename instead. Decoding is limited to 10 MiB and 32 megapixels per image, with a 64 MiB API page limit. `NO_COLOR` keeps attachment labels without previews. Requires the Mac API's `attachments` fields.

## Refresh behavior

Background requests refresh every 30 seconds and when opening a chat. Messages and wrapped rows stay cached per conversation for immediate reopening; unchanged polls do not rewrap. Network requests never block input. Failed requests keep cached content. While reading earlier messages, refresh preserves the visible message and shows a new-message count. No streaming or notifications.

Refresh updates the newest message pages and keeps loaded older history. Edits or deletions outside those refreshed pages can remain cached until restarting. Timestamps show UTC date and minute.

Emoji widths are calibrated before the first frame when explicit terminal widths are unavailable, with at most two 50 ms cursor queries. For terminals that misreport widths, try `VAXIS_FORCE_UNICODE=1` or `VAXIS_FORCE_WCWIDTH=1`. Ctrl+L forces a repaint. `NO_COLOR` disables color.

## Checks

```sh
go test -race ./...
go vet ./...
python3 -m venv .venv
.venv/bin/pip install pyte pillow
.venv/bin/python tests/smoke.py bin/natterwire-tui
.venv/bin/python tests/images.py bin/natterwire-tui
.venv/bin/python tests/latency.py bin/natterwire-tui
```

The smoke test runs the binary in a terminal against synthetic HTTP data, including a real 30-second poll. Add `--captures ../.amp-review/tui` to capture terminal screens and ANSI recordings. Latency measures key injection to completed ANSI frame, not terminal presentation or remote transport. `tests/emoji.py` exercises emoji scrolling; `tests/render.cjs` replays recordings in browser xterm.js (dependencies and usage in its header).
