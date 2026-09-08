# Frontend

Vanilla client embedded in the API. Chats and messages stay in memory, and drafts disappear on reload.

Messages linkify HTTP(S) and `www.` URLs and show one clickable card per message. Visible cards request `/link-preview?url=…`; the API reads Open Graph/Twitter metadata without running page scripts, with HTML title fallback. Private-network targets are blocked. Metadata requests contact the linked site from the API host; preview images load directly in the browser without a referrer. Sites requiring login or blocking previews retain a domain/path card. This does not decode Apple's stored rich-link payloads or embed social widgets.

Install from the browser's app menu on the Mac's HTTPS Tailscale URL. On iPhone/iPad, use Safari → Share → Add to Home Screen. Plain HTTP on a tailnet IP is not a secure install origin. The installed app still needs the API online; there is no offline cache or send queue. Manifest icons are resized from `macos/Natterwire/Assets/NatterwireIcon.png`.

The visible app refreshes chats and the open transcript every 30 seconds, and on focus, return from the background, or reconnection. Drafts, search and reading position survive refresh. Hidden apps do not poll; the OS can suspend them. This is client polling, not a background PWA scheduling guarantee.

An unread dot indicates incoming messages with `is_read=0` on the Mac, excluding filtered system/deleted messages. No count is displayed. A missing source column means unknown, not zero. Opening a conversation here does **not** mark it read in iMessage. This uses an undocumented database schema, so manual conversation-level unread flags and macOS version differences may not match the dot exactly.

On first launch in a supported browser, a dismissible **Enable notifications** prompt offers opt-in. Only that click requests permission. Dismissal is remembered in local storage; after permission is handled, no notification settings panel remains. Manage permission later in browser or device settings, then reopen the app. Clearing site data resets the prompt. Notifications say only “New message”; clicking opens the chat. On iPhone/iPad, use the Home Screen app on iOS/iPadOS 16.4 or later. The Mac must stay awake, connected, and running Natterwire. OS permissions, Focus settings and push-service availability can delay or suppress delivery. See [Web Push state and limitations](../push.md).

## Checks

After opening the fixture client in `agent-browser`, follow the invocation at the top of [`frontend.js`](../tests/frontend.js). It includes a real 30-second poll and checks cached switching, unread changes, drafts, search and image reuse. Reported layout timings exclude presentation and network latency. Reload afterward.

With a temporary `--push-state /tmp/natterwire-test/push.json`, wait for notification setup to finish, then run `agent-browser eval "$(cat api/tests/notifications.js)"`. It mocks permission/subscription APIs and makes no real push registration. Run `node --test api/tests/service-worker.cjs` for worker events, privacy and click routing. Native permission UI and vendor-to-device delivery require separate authorized device tests.
