# Verification

Run `go test -race ./...` and `go vet ./...` in both `api` and `tui`. See [API fixtures and macOS limitations](api/README.md) and [TUI checks](tui/README.md#checks). Mac-only checks must use isolated source/build directories, never modify a shared checkout, and must not install or restart services without deployment authorization.

# Shipping

Follow [.agents/ship.md](.agents/ship.md), including deploying changed TUI and API components on the Mac.
