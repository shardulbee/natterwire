// node --test api/tests/service-worker.cjs
const { test } = require('node:test');
const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { join } = require('node:path');
const vm = require('node:vm');

test('push always displays a private notification; clicks focus or open the correct chat', async () => {
  const handlers = {}, shown = [], messages = [], opened = [], fetched = [];
  let windows = [], focused = false, closed = false;
  const self = {
    location: { origin: 'https://fixture.invalid' },
    addEventListener: (type, handler) => { handlers[type] = handler; },
    skipWaiting: async () => {},
    registration: { showNotification: async (...args) => { shown.push(args); } },
    clients: { claim: async () => {}, matchAll: async () => windows, openWindow: async url => { opened.push(url); } },
  };
  vm.runInNewContext(readFileSync(join(__dirname, '../web/sw.js'), 'utf8'), { self, URL, fetch: async (url, options) => { fetched.push(options); return { ok: true }; } });
  const dispatch = async (type, data) => {
    let pending;
    handlers[type]({ ...data, waitUntil: promise => { pending = promise; } });
    await pending;
  };
  assert.equal(handlers.fetch, undefined, 'must not cache private API data or sends');
  await dispatch('install');
  await dispatch('activate');
  windows = [{ url: 'https://fixture.invalid/', postMessage: data => messages.push(data), focus: async () => { focused = true; } }];
  await dispatch('push', { data: { json: () => ({ chat: 'fixture-chat', text: 'Never display this', sender: 'Private name' }) } });
  assert.equal(shown[0][0], 'Natterwire');
  assert.equal(shown[0][1].body, 'New message');
  assert.equal(shown[0][1].data.chat, 'fixture-chat');
  assert.equal(messages[0].type, 'refresh');
  assert.ok(!JSON.stringify(shown).includes('Private name'));
  await dispatch('notificationclick', { notification: { data: { chat: 'fixture-chat' }, close: () => { closed = true; } } });
  assert.ok(closed && focused);
  assert.equal(messages[1].chat, 'fixture-chat');
  assert.equal(opened.length, 0);
  windows = [];
  await dispatch('notificationclick', { notification: { data: { chat: 'fixture-chat' }, close() {} } });
  assert.equal(opened[0], '/?chat=fixture-chat');
  await dispatch('notificationclick', { notification: { data: { chat: 'https://evil.invalid' }, close() {} } });
  assert.equal(opened[1], '/');
  await dispatch('push', { data: { json() { throw new Error('bad data'); } } });
  assert.equal(shown.length, 2, 'malformed payload must still display a visible notification');
  await dispatch('pushsubscriptionchange', { oldSubscription: { endpoint: 'old' }, newSubscription: { endpoint: 'new' } });
  assert.equal(fetched.map(request => request.method).join(), 'POST,DELETE');
  await dispatch('pushsubscriptionchange', { oldSubscription: { endpoint: 'same' }, newSubscription: { endpoint: 'same' } });
  assert.equal(fetched.map(request => request.method).join(), 'POST,DELETE,POST');
});
