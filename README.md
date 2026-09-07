<p align="center">
  <img src="macos/Natterwire/Assets/NatterwireIcon.png" alt="Natterwire fox switchboard icon" width="180">
</p>

# Natterwire

Read and send Messages from your terminal. Mac backend, macOS/Linux client.

## Install

Requires Go 1.25+ and Xcode command-line tools on the Mac.

```sh
scripts/install.sh
```

## Use

Open Natterwire on the Mac, then run:

```sh
natterwire-tui
```

For remote access, use Tailscale. The API has no authentication; restrict access to trusted devices. Never expose it publicly.

[API setup](api/README.md) · [TUI usage](tui/README.md) · [Checks](AGENTS.md)
