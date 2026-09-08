// Registration and key fetching happen before the click. Safari requires subscribe()
// to start directly in the user gesture, not after an unrelated network request.
let pushRegistration, pushKey, pushDismissed = false;
const pushButton = $('notifications'), pushStatus = $('notification-status');
const pushPrompt = $('notification-prompt'), dismissPush = $('dismiss-notifications');
try { pushDismissed = localStorage.getItem('natterwire-notifications-dismissed') === 'true'; } catch {}

async function saveSubscription(subscription) {
  return request('push', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(subscription) });
}

function dismissNotifications() {
  pushDismissed = true;
  pushPrompt.hidden = true;
  try { localStorage.setItem('natterwire-notifications-dismissed', 'true'); } catch {}
}
dismissPush.onclick = dismissNotifications;

async function prepareNotifications() {
  pushPrompt.hidden = true;
  if (!isSecureContext || !('serviceWorker' in navigator) || !('PushManager' in window) || !('Notification' in window) || Notification.permission === 'denied') return;
  try {
    await navigator.serviceWorker.register('sw.js', { updateViaCache: 'none' });
    pushRegistration = await navigator.serviceWorker.ready;
    const config = await request('push');
    pushKey = Uint8Array.from(atob(config.publicKey.replaceAll('-', '+').replaceAll('_', '/')), char => char.charCodeAt(0));
    let subscription = await pushRegistration.pushManager.getSubscription();
    // Permission may have been enabled later in browser/device settings.
    if (!subscription && Notification.permission === 'granted') subscription = await pushRegistration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: pushKey });
    if (subscription) {
      await saveSubscription(subscription);
      dismissNotifications();
    } else pushPrompt.hidden = pushDismissed;
  } catch {
    // Retry setup on the next load without adding permanent settings or errors.
  }
}

pushButton.onclick = async () => {
  pushButton.disabled = dismissPush.disabled = true;
  pushStatus.hidden = true;
  try {
    const subscription = await pushRegistration.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: pushKey });
    await saveSubscription(subscription);
    dismissNotifications();
  } catch {
    if (Notification.permission !== 'granted') dismissNotifications();
    else {
      pushStatus.textContent = 'Couldn’t enable notifications. Try again.';
      pushStatus.hidden = false;
    }
  } finally {
    pushButton.disabled = dismissPush.disabled = false;
  }
};

if ('serviceWorker' in navigator) navigator.serviceWorker.addEventListener('message', event => {
  if (event.data?.type === 'open-chat') openNotificationChat(event.data.chat);
  if (event.data?.type === 'refresh') refreshApp();
});
prepareNotifications();
