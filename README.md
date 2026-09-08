<p align="center">
  <img src="macos/Natterwire/Assets/NatterwireIcon.png" alt="Natterwire fox switchboard icon" width="180">
</p>

# Natterwire

Read and send Messages from your browser, installed PWA, or macOS/Linux terminal. Requires a Mac running the backend.

Build and install on the Mac with Go 1.25+ and Xcode command-line tools:

```sh
scripts/install.sh
```

Open Natterwire on the Mac, then use its HTTPS Tailscale URL in your browser or run `natterwire-tui`. Install the PWA from the browser's app menu, or Safari → Share → Add to Home Screen on iOS. The Mac backend must stay online.

Use Tailscale for remote access. The API has no authentication; allow only trusted devices, never public access.

[API setup](api/README.md) · [Browser/PWA](api/web/README.md) · [TUI usage](tui/README.md) · [Checks](AGENTS.md)
