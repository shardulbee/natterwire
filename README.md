<p align="center">
  <img src="macos/Natterwire/Assets/NatterwireIcon.png" alt="Natterwire fox switchboard icon" width="180">
</p>

# Natterwire

Read and send Messages from your browser, installed PWA, or macOS/Linux terminal. Requires a Mac running the backend.

Build and install on the Mac with Go 1.25+ and Xcode command-line tools:

```sh
scripts/install.sh
```

Open Natterwire on the Mac, then visit `http://127.0.0.1:8741` on that Mac or run `natterwire-tui`. The Mac backend must stay online.

Remote access is yours to configure; Natterwire does not set up networking or HTTPS. The API has no authentication, so restrict access to trusted devices and never expose it publicly. Installing the browser client as a PWA remotely requires HTTPS. See [API setup](api/README.md) for an optional Tailscale example.

[API setup](api/README.md) · [Browser/PWA](api/web/README.md) · [TUI usage](tui/README.md) · [Checks](AGENTS.md)
