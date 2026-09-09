// Run on a fresh fixture page after notification setup finishes:
// agent-browser eval "$(cat api/tests/notifications.js)"
// Subscription/permission APIs are mocked. No real permission, subscription, or push.
(async () => {
  const assert = (value, message) => { if (!value) throw new Error(message); };
  const storageKey = 'natterwire-notifications-dismissed';
  const original = { request, pushRegistration, pushKey, pushDismissed, stored: localStorage.getItem(storageKey), worker: Object.getOwnPropertyDescriptor(navigator, 'serviceWorker'), permission: Object.getOwnPropertyDescriptor(Notification, 'permission') };
  let subscriptions = 0, permission = 'default', refusal = '', offline = false, subscribed = null;
  const calls = [];
  const sub = { endpoint: 'https://fcm.googleapis.com/fixture', keys: { auth: 'fixture', p256dh: 'fixture' } };
  const registration = { pushManager: {
    getSubscription: async () => subscribed,
    subscribe: async options => {
      subscriptions++;
      assert(options.userVisibleOnly && options.applicationServerKey === pushKey, 'Subscription must use the server VAPID key');
      permission = refusal || 'granted';
      if (refusal) throw new Error('Permission not granted');
      return subscribed = sub;
    },
  } };
  try {
    pushDismissed = false;
    localStorage.removeItem(storageKey);
    Object.defineProperty(Notification, 'permission', { configurable: true, get: () => permission });
    Object.defineProperty(navigator, 'serviceWorker', { configurable: true, value: { register: async () => registration, ready: Promise.resolve(registration) } });
    request = async (path, options) => {
      assert(path === 'push', 'Unexpected request');
      if (!options) return { publicKey: 'AQ' };
      calls.push(options.method);
      if (offline) throw new Error('Offline');
      return { ok: true };
    };
    await prepareNotifications();
    assert(!pushPrompt.hidden && subscriptions === 0, 'First launch must show only an opt-in prompt, not request permission');
    dismissPush.click();
    assert(pushPrompt.hidden && localStorage.getItem(storageKey) === 'true', 'Dismissal must hide and persist');
    await prepareNotifications();
    assert(pushPrompt.hidden && subscriptions === 0, 'Dismissed prompt must not return');
    pushDismissed = false;
    await prepareNotifications();
    const pending = pushButton.onclick();
    assert(subscriptions === 1, 'subscribe must start synchronously in the click handler');
    await pending;
    assert(pushPrompt.hidden && pushDismissed, 'Successful opt-in must remove the prompt, not show settings');
    assert(calls.join() === 'POST', 'Opt-in must persist subscription');
    await prepareNotifications();
    assert(pushPrompt.hidden && subscriptions === 1 && calls.length === 2, 'Existing subscription must re-sync without prompting');
    subscribed = null;
    await prepareNotifications();
    assert(pushPrompt.hidden && subscriptions === 2, 'Already granted browser permission must restore subscription without a prompt');
    for (refusal of ['denied', 'default']) {
      permission = 'default';
      subscribed = null;
      pushDismissed = false;
      await prepareNotifications();
      await pushButton.onclick();
      assert(pushPrompt.hidden && pushDismissed, 'Handling the system permission prompt must remove our prompt');
      await prepareNotifications();
      assert(pushPrompt.hidden, 'Permission refusal must not produce a persistent settings panel');
    }
    refusal = '';
    permission = 'default';
    pushDismissed = false;
    await prepareNotifications();
    offline = true;
    await pushButton.onclick();
    assert(!pushPrompt.hidden && !pushButton.disabled && !pushStatus.hidden, 'Failed persistence after permission must allow retry');
    offline = false;
    await pushButton.onclick();
    assert(pushPrompt.hidden, 'Successful retry must remove the prompt');

    // Notification clicks focus the target without discarding another chat’s draft.
    const active = conversations[selected];
    $('draft').value = 'Retain notification draft';
    $('draft').dispatchEvent(new Event('input'));
    const target = conversations.find(chat => chat !== active);
    request = async () => ({ items: [] });
    await openNotificationChat(target.id);
    assert(conversations[selected] === target && drafts.get(active.id) === 'Retain notification draft', 'Notification navigation lost chat or draft');
    const unknown = 'fixture-notification-chat';
    request = async path => path.startsWith('chats?') ? { items: [{ id: unknown, displayName: 'Older notification', unreadCount: 1, latestIncomingRowID: '500' }], nextBefore: null } : { items: [] };
    await openNotificationChat(unknown);
    assert(conversations[selected].id === unknown, 'Notification must find a conversation outside the initial list');
    assert(buttons[selected].getAttribute('aria-label') === 'Older notification, unread in Natterwire', 'Notification must not acknowledge messages that have not loaded');
    await openNotificationChat('https://evil.invalid/');
    assert(conversations[selected].id === unknown, 'Invalid notification data must not navigate');
    return { result: 'PASS', checks: 'first-launch prompt, remembered dismissal, immediate gesture, permission refusal, existing permission, persistence retry, chat navigation, unread state' };
  } finally {
    request = original.request;
    pushRegistration = original.pushRegistration;
    pushKey = original.pushKey;
    pushDismissed = original.pushDismissed;
    if (original.stored === null) localStorage.removeItem(storageKey);
    else localStorage.setItem(storageKey, original.stored);
    if (original.worker) Object.defineProperty(navigator, 'serviceWorker', original.worker);
    else delete navigator.serviceWorker;
    if (original.permission) Object.defineProperty(Notification, 'permission', original.permission);
    else delete Notification.permission;
  }
})()
