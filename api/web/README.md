# Frontend

Vanilla client embedded in the API. Chats and messages stay in memory, and drafts disappear on reload.

Messages linkify HTTP(S) and `www.` URLs and show one clickable card per message. Visible cards request `/link-preview?url=…`; the API reads Open Graph/Twitter metadata without running page scripts, with HTML title fallback. Private-network targets are blocked. Metadata requests contact the linked site from the API host; preview images load directly in the browser without a referrer. Sites requiring login or blocking previews retain a domain/path card. This does not decode Apple's stored rich-link payloads or embed social widgets.

Install from the browser's app menu on the Mac's HTTPS Tailscale URL. On iPhone/iPad, use Safari → Share → Add to Home Screen. Plain HTTP on a tailnet IP is not a secure install origin. The installed app still needs the API online; there is no offline cache or send queue. Manifest icons are resized from `macos/Natterwire/Assets/NatterwireIcon.png`.

After opening the fixture client in `agent-browser`, run `agent-browser eval "$(cat api/tests/frontend.js)"` from the repository root. It checks cached chat switching, drafts, search, refresh, and image reuse, and reports synchronous switch/layout timings for 100-message chats. Reload afterward. Timings exclude display presentation and network latency.
