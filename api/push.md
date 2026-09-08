# Web Push

`push.go` polls the read-only Messages connection every 30 seconds while the API runs. Each device starts at the latest message row on opt-in, so installing does not notify for old history. Subsequent incoming, eligible rows trigger encrypted RFC 8291 Web Push. Messages already read at poll time are skipped when `is_read` is available. Outgoing messages, reactions, system/deleted items and archived chats are excluded. No messages are sent through Messages.app by this worker.

## State and delivery

The default state is `~/Library/Application Support/Natterwire/push.json` on macOS, or `$XDG_CONFIG_HOME/Natterwire/push.json` on Linux. `--push-state PATH` overrides it; an empty value disables push. Use a separate temporary path for fixtures. The owner-only file stores persistent VAPID keys, up to 16 browser subscriptions, and a cursor per subscription. It is atomically replaced and separate from `chat.db`. Do not commit, serve, or print it. Keep one API process per state file. Invalid/unwritable state disables notifications without blocking Messages access.

The payload contains only the encrypted chat identifier. The worker displays “Natterwire / New message”, with no sender or message preview. Vendor services still see endpoints, timing and traffic volume. Requests use HTTPS to Apple, Google, Mozilla or Windows push hosts, with no redirects or private-network dialing. No Apple Developer account or custom notification relay is required.

Each poll processes at most 100 eligible incoming rows per device. Successful batches persist their cursor; failed delivery retries next tick, independently per device. Partial success or a crash before persistence can cause duplicates. Notifications for the same chat share a tag. HTTP 404/410 removes expired subscriptions. Vendors can retain undelivered pushes for at most the requested 5-minute TTL; acceptance is not proof of device delivery. Disable notifications in browser or device settings. Revoking display permission does not necessarily delete the server subscription immediately; expired endpoints are removed on the next delivery attempt.

The Mac cannot detect or send new messages while asleep or offline. On return it catches up with still-unread new rows. Browser background suspension stops foreground refresh but does not require the PWA to be open for Web Push. Permission, Focus, connectivity, browser support and OS policy still govern display. There is no silent push or background-sync guarantee. Opening the app re-registers an existing subscription; `pushsubscriptionchange` support varies. If the source database is replaced/restored with different row IDs, remove each affected subscription with `DELETE /push`, then reopen the app to register it with a fresh cursor.

## HTTP contract

Same trust boundary as sends: local processes and devices allowed by the Mac's Tailscale policy. Do not expose the API publicly.

- `GET /push` returns only `{ "publicKey": "..." }`. No endpoints or private key.
- `POST /push` accepts browser subscription JSON with `endpoint` and `keys.auth`/`keys.p256dh`. It validates vendor, key lengths/curve and an 8 KiB body limit, then upserts without resetting an existing cursor.
- `DELETE /push` accepts `{ "endpoint": "..." }`, removes it idempotently and persists.
- Mutations require `application/json`; cross-origin browser requests are rejected. Successful mutations return `{ "ok": true }`.

## iMessage read-state boundary

`message.is_read` on **incoming** rows is the source for `Chat.unreadCount`. Outgoing `is_read` is a recipient receipt and must not be counted. Missing `is_read` yields `null`. No API route marks read, and opening a conversation issues reads only. Conversation-level “Mark as Unread” and other undocumented schema details are not guaranteed to match these message-level counts.

Apple documents [mark-read in the Messages UI](https://support.apple.com/guide/messages/mark-a-conversation-as-unread-ichtf9781355/mac), not a public scripting command. BlueBubbles' [markRead implementation](https://github.com/BlueBubblesApp/bluebubbles-server/blob/master/packages/server/src/server/api/privateApi/apis/PrivateApiChat.ts) uses its private helper. Natterwire does not use private frameworks, Accessibility menu automation or direct database writes to simulate read synchronization. See also [imessage-exporter's schema notes](https://github.com/ReagentX/imessage-exporter/blob/main/docs/tables/messages.md) and [Apple's Web Push requirements](https://developer.apple.com/documentation/usernotifications/sending-web-push-notifications-in-web-apps-and-browsers).
