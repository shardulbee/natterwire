# Natterwire TUI

macOS/Linux terminal client using [Vaxis](third_party/VAXIS.md). Requires Go 1.25+ to build and a terminal at least 60 × 14. See [API setup and access](../api/README.md).

```sh
# From tui/:
go build -o bin/natterwire-tui .
./bin/natterwire-tui --demo
./bin/natterwire-tui --url https://mac.example.ts.net
```

`NATTERWIRE_URL` sets the default URL; otherwise `http://127.0.0.1:8741`. Demo mode never sends. For cross-compilation, use `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build`; substitute `arm64` as needed.

## Keys

| Focus | Keys |
| --- | --- |
| Sidebar | `j/k` or arrows: chats; Ctrl+U/Ctrl+D: scroll; `G`: latest; `i`: compose |
| Insert | Type; Esc: sidebar; Enter: send |
| Outside insert | PageUp/PageDown: scroll; End: latest; `r`: refresh; `q`: quit |
| Anywhere | Ctrl+L: repaint; Ctrl+C: quit |

Chats sort by latest activity and refresh every 30 seconds. Scroll to load older messages or more chats. Drafts survive chat switches, not app exits. Failed sends retain their request ID for retries; after an ambiguous failure, check Messages before restarting or resending. Acceptance does not confirm delivery.

Images require Kitty graphics; other terminals show `[Image: filename]`. For emoji width problems, try `VAXIS_FORCE_UNICODE=1` or `VAXIS_FORCE_WCWIDTH=1`. `NO_COLOR` disables color. Timestamps use UTC.

## Checks

Run the [required Go checks](../AGENTS.md). From `tui/`, build the binary above, then:

```sh
python3 -m venv .venv
.venv/bin/pip install pyte pillow
.venv/bin/python tests/smoke.py bin/natterwire-tui
.venv/bin/python tests/images.py bin/natterwire-tui
.venv/bin/python tests/latency.py bin/natterwire-tui
# Requires Kitty, Xvfb, xauth, and ImageMagick:
xvfb-run -a .venv/bin/python tests/images.py bin/natterwire-tui --kitty
```

See [API integration checks](../api/README.md#tests), [emoji checks](tests/emoji.py), and [capture renderer instructions](tests/render.cjs). Latency checks measure ANSI output, not screen presentation. Refresh, input, and caching behavior live in [main.go](main.go) and [its tests](main_test.go).
