# Frontend

Browser client embedded in the [API](../README.md). Chats and drafts stay in memory; reload discards drafts. No offline cache or send queue.

Link previews contact linked sites from the API host; images load directly in the browser without a referrer. Private-network targets are blocked. See [preview handling](../link_preview.go).

For remote PWA installation, configure HTTPS access to the Mac yourself, then install from the browser's app menu, or Safari → Share → Add to Home Screen on iOS. Plain HTTP remote addresses do not support installation. The API must remain online. See [access setup](../README.md#macos-setup).

The visible app refreshes every 30 seconds and on focus, return, or reconnection, preserving drafts, search, and reading position. Hidden apps do not poll.

Unread dots are browser-local. Each chat starts from the Mac's unread status, then stores a read-through message row in localStorage. Viewing the bottom of a focused, visible conversation acknowledges loaded history, including when replying. Background or scrolled-up chats remain unread. New incoming rows restore the dot; outgoing messages do not. Clearing site data resets this state; browsers do not share it, and Apple Messages/read receipts are unchanged. Cursors assume the same growing Messages database; reset site data after replacing it.

Click Enable notifications to opt in; dismissal is remembered until site data is cleared. Manage permission later in browser/device settings, then reopen the app. Notifications say only "New message" and open the chat. iOS/iPadOS requires a Home Screen app and version 16.4+. The Mac must stay awake, online, and running Natterwire; OS settings and push services can delay or suppress delivery. See [Web Push state and limitations](../push.md).

## Checks

For a live incoming-message demo, run [`unread-fixture.py`](../tests/unread-fixture.py) with a fresh scratch directory, then point the API at its `chat.db`. It seeds unread chats and inserts into the test group every 30 seconds. Stop the feeder when finished. Linux still cannot send real messages.

Open the [fixture client](../README.md#linux-fixture-workflow) in `agent-browser` and follow [frontend.js](../tests/frontend.js). It checks a real 30-second poll, cached switching, unread changes, drafts, search, and image reuse. Timings exclude screen presentation and network latency. Reload afterward.

Run [`unread.js`](../tests/unread.js) in the fixture browser for local cursor persistence, focus/visibility, scrolling, unloaded arrivals and exact row-ID comparisons. Reload afterward.

For notification checks, use a temporary `--push-state /tmp/natterwire-test/push.json`, wait for setup, then run `agent-browser eval "$(cat api/tests/notifications.js)"` from the repo root. It mocks permissions/subscriptions without real registration. Run `node --test api/tests/service-worker.cjs` for worker events, privacy, and click routing. Native permission UI and device delivery need separate authorized device tests.
