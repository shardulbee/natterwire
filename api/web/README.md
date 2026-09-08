# Frontend

Browser client embedded in the [API](../README.md). Chats and drafts stay in memory; reload discards drafts. No offline cache or send queue.

Link previews contact linked sites from the API host; images load directly in the browser without a referrer. Private-network targets are blocked. See [preview handling](../link_preview.go).

Install from the browser's app menu at the Mac's HTTPS Tailscale URL, or Safari → Share → Add to Home Screen on iOS. Plain HTTP tailnet IPs do not support installation. The API must remain online.

The visible app refreshes every 30 seconds and on focus, return, or reconnection, preserving drafts, search, and reading position. Hidden apps do not poll.

Unread dots reflect incoming unread messages on the Mac, not counts. Missing read-state data means unknown. Opening a chat does not mark it read in iMessage; manual unread flags and macOS schema differences may not match.

Click Enable notifications to opt in; dismissal is remembered until site data is cleared. Manage permission later in browser/device settings, then reopen the app. Notifications say only "New message" and open the chat. iOS/iPadOS requires a Home Screen app and version 16.4+. The Mac must stay awake, online, and running Natterwire; OS settings and push services can delay or suppress delivery. See [Web Push state and limitations](../push.md).

## Checks

Open the [fixture client](../README.md#linux-fixture-workflow) in `agent-browser` and follow [frontend.js](../tests/frontend.js). It checks a real 30-second poll, cached switching, unread changes, drafts, search, and image reuse. Timings exclude screen presentation and network latency. Reload afterward.

For notification checks, use a temporary `--push-state /tmp/natterwire-test/push.json`, wait for setup, then run `agent-browser eval "$(cat api/tests/notifications.js)"` from the repo root. It mocks permissions/subscriptions without real registration. Run `node --test api/tests/service-worker.cjs` for worker events, privacy, and click routing. Native permission UI and device delivery need separate authorized device tests.
