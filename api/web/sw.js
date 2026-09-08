// No fetch handler or offline cache: private conversations always come from the API.
self.addEventListener('install', event => event.waitUntil(self.skipWaiting()));
self.addEventListener('activate', event => event.waitUntil(self.clients.claim()));

self.addEventListener('push', event => {
  let chat = '';
  try { chat = event.data.json().chat; } catch {}
  if (typeof chat !== 'string' || !/^[A-Za-z0-9_-]{1,2048}$/.test(chat)) chat = '';
  // Safari requires every push to produce a visible notification, even when open.
  event.waitUntil(Promise.all([
    self.registration.showNotification('Natterwire', {
      body: 'New message', icon: '/icon-192.png', tag: chat || 'natterwire', data: { chat },
    }),
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then(clients => {
      for (const client of clients) client.postMessage({ type: 'refresh' });
    }),
  ]));
});

self.addEventListener('notificationclick', event => {
  event.notification.close();
  const chat = event.notification.data?.chat;
  const valid = typeof chat === 'string' && /^[A-Za-z0-9_-]{1,2048}$/.test(chat);
  event.waitUntil((async () => {
    for (const client of await self.clients.matchAll({ type: 'window', includeUncontrolled: true })) {
      if (new URL(client.url).origin !== self.location.origin) continue;
      if (valid) client.postMessage({ type: 'open-chat', chat });
      await client.focus();
      return;
    }
    await self.clients.openWindow(valid ? `/?chat=${encodeURIComponent(chat)}` : '/');
  })());
});

self.addEventListener('pushsubscriptionchange', event => {
  // Browser support varies. Opening the app also re-registers an existing subscription.
  if (!event.newSubscription) return;
  event.waitUntil((async () => {
    const response = await fetch('/push', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(event.newSubscription) });
    if (response.ok && event.oldSubscription && event.oldSubscription.endpoint !== event.newSubscription.endpoint) await fetch('/push', { method: 'DELETE', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(event.oldSubscription) });
  })());
});
