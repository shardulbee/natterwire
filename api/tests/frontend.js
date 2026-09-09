// Run in a fixture browser page. The real polling test takes at least 31 seconds:
// agent-browser eval "{ ($(cat api/tests/frontend.js)).then(r => window.frontendResult = r, e => window.frontendResult = {error: e.message}); 'started'; }"
// After 35 seconds: agent-browser eval 'window.frontendResult'
// Replaces in-memory conversations and mocks sends. Does not send real messages. Reload afterward.
(async () => {
  const assert = (condition, message) => { if (!condition) throw new Error(message); };
  const frame = () => new Promise(resolve => requestAnimationFrame(resolve));
  for (const key of Object.keys(localStorage)) if (key.startsWith('read-through:fixture-')) localStorage.removeItem(key);
  readThrough.clear();
  conversations = Array.from({ length: 40 }, (_, i) => ({
    id: `fixture-${i}`, name: `Test conversation ${i + 1}`, unreadCount: i % 3, latestIncomingRowID: '100', preview: 'Performance fixture', time: 'Today',
    messages: Array.from({ length: 100 }, (_, j) => [
      j % 2 ? 'You' : `Test conversation ${i + 1}`, '12:30',
      `Message ${j + 1}: Testing switching, scrolling and draft input with a full conversation.`,
      { id: String(j), rowID: String(j + 1), sentAt: '2026-09-08T12:30:00Z' },
    ]),
  }));
  buildChats();
  select(0);
  const first = $('messages').querySelector('.message');
  const samples = [];
  for (let i = 0; i < 60; i++) {
    const start = performance.now();
    select(i % 2);
    void $('transcript').scrollHeight;
    samples.push(performance.now() - start);
  }
  select(0);
  assert($('messages').querySelector('.message') === first, 'Reopening must reuse message DOM');
  assert(getComputedStyle(first).animationName === 'none', 'History must not replay entrance animations');
  assert(document.querySelectorAll('[aria-current="true"]').length === 1, 'Exactly one selected chat');
  $('draft').value = 'Draft survives switching';
  $('draft').dispatchEvent(new Event('input'));
  select(1);
  select(0);
  assert($('draft').value === 'Draft survives switching', 'Draft lost');
  assert(buttons[0].querySelector('.unread').hidden, 'Read chat must not show an unread marker');
  assert(buttons[1].getAttribute('aria-label') === 'Test conversation 2', 'Viewing latest messages must clear local unread state');
  const dot = buttons[1].querySelector('.unread');
  const summaryLeft = buttons[1].querySelector('.chat-summary').getBoundingClientRect().left;
  for (const count of [1, 120, 0]) {
    conversations[1].latestIncomingRowID = String(100 + count);
    updateChat(1);
    assert(dot.hidden === !(count > 0) && dot.textContent === '', 'Unread must be a binary dot, never a number');
    assert(buttons[1].querySelector('.chat-summary').getBoundingClientRect().left === summaryLeft, 'Unread changes must not shift conversation text');
    if (count > 0) assert(dot.getBoundingClientRect().width === 8 && dot.getBoundingClientRect().height === 8, 'Unread dot must retain its size for any count');
  }
  conversations[1].unreadCount = 1;
  conversations[1].latestIncomingRowID = '100';
  updateChat(1);
  await frame();
  $('transcript').scrollTop = 0;
  $('draft').dispatchEvent(new Event('input'));
  await frame();
  assert($('transcript').scrollTop === 0, 'Typing must not move a scrolled transcript');
  openSearch();
  $('search').value = 'conversation 2';
  filterChats();
  assert(visible.length === 11, 'Search results changed');
  const searchMutations = new MutationObserver(() => {});
  searchMutations.observe($('chats'), { attributes: true, childList: true, subtree: true, characterData: true });
  filterChats();
  assert(searchMutations.takeRecords().length === 0, 'Repeated search must not rewrite unchanged rows');
  searchMutations.disconnect();
  closeSearch();
  select(1);
  renderMessages(conversations[0]);
  assert($('messages').querySelector('.message') !== first, 'Background chat must not replace active transcript');

  // A loaded attachment keeps its image node and source across chat switches.
  conversations[0].messages.push(['You', '12:30', '', {
    sentAt: '2026-09-08T12:30:00Z', attachments: [
      { mediaID: 'fixture', version: '1', width: 64, height: 64, mimeType: 'image/png', filename: 'Fixture image' },
    ],
  }]);
  select(0);
  const imageFrame = $('messages').querySelector('.attachment-frame');
  mediaObserver.unobserve(imageFrame);
  imageFrame.mediaItem.url = 'favicon.png?fixture=1';
  mediaQueue.push(imageFrame.mediaItem);
  loadQueuedMedia();
  for (let i = 0; i < 120 && imageFrame.disabled; i++) await frame();
  assert(!imageFrame.disabled, 'Fixture image failed to load');
  const image = imageFrame.firstElementChild;
  const source = image.src;
  select(1);
  select(0);
  assert($('messages').querySelector('.attachment-image') === image && image.src === source, 'Image must survive reopening');
  imageFrame.click();
  assert($('lightbox').open, 'Cached image lightbox must open');
  $('lightbox').close();
  await frame();
  await frame();
  assert(image.parentNode === imageFrame, 'Lightbox must return the cached image');

  // New page tuples invalidate old DOM without keeping stale text.
  conversations[0].messages = [['You', '12:30', 'Updated message', { sentAt: '2026-09-08T12:30:00Z' }]];
  renderMessages(conversations[0]);
  assert($('messages').querySelectorAll('.message').length === 1 && $('messages').textContent.includes('Updated message'), 'Refresh must replace old content');

  // Hold a metadata response while repeated refreshes and scrolling happen.
  const originalRequest = request;
  let release, calls = 0, fail = false;
  let gate = new Promise(resolve => { release = resolve; });
  const page = { items: Array.from({ length: 100 }, (_, i) => ({
    id: String(100 - i), text: `Refresh fixture message ${100 - i}`, sender: 'Test conversation 3',
    sentAt: `2026-09-08T12:${String(59 - Math.floor(i / 2)).padStart(2, '0')}:00Z`, attachments: [],
  })) };
  request = async () => { calls++; await gate; if (fail) throw new Error('Fixture failure'); return structuredClone(page); };
  try {
    select(2);
    const chat = conversations[2];
    const pending = loadMessages(chat, true);
    for (let i = 0; i < 9; i++) assert(loadMessages(chat, true) === pending, 'Concurrent refreshes must share a promise');
    assert(calls === 1, 'Ten concurrent refreshes must issue one request');
    release();
    await pending;
    await frame();
    const before = [...$('messages').querySelectorAll('.message')];
    let mutationCount = 0;
    const mutations = new MutationObserver(records => { mutationCount += records.length; });
    mutations.observe($('messages'), { childList: true, subtree: true, characterData: true });
    await loadMessages(chat, true);
    assert(mutationCount + mutations.takeRecords().length === 0, 'Unchanged refresh must not mutate transcript DOM');
    mutations.disconnect();
    assert($('messages').querySelector('.message') === before[0], 'Unchanged page must preserve message elements');

    gate = new Promise(resolve => { release = resolve; });
    const refresh = loadMessages(chat, true);
    $('transcript').scrollTop = 150;
    page.items[0].text = 'Edited newest message';
    release();
    await refresh;
    assert(Math.abs($('transcript').scrollTop - 150) < 2, 'Refresh must preserve scrolling done during the request');
    assert($('messages').querySelector('.message') === before[0], 'Editing one message must reuse unchanged siblings');
    assert($('messages').textContent.includes('Edited newest message'), 'Edited message not rendered');
    fail = true;
    await loadMessages(chat, true).then(() => { throw new Error('Expected fixture failure'); }, () => {});
    assert(!chat.loading, 'Failure must release the in-flight request');
    fail = false;
    await loadMessages(chat, true);
  } finally { request = originalRequest; }

  // Automatic refresh preserves the reader, drafts and search while discovering chats.
  const active = conversations[selected];
  const listed = conversations.map(chat => ({ id: chat.id, displayName: chat.name, unreadCount: chat.unreadCount, latestIncomingRowID: chat.latestIncomingRowID, lastMessageAt: '2026-09-08T13:00:00Z' }));
  listed.unshift({ id: 'new-chat', displayName: 'New arrival', lastMessageAt: '2026-09-08T14:00:00Z' });
  let polls = 0, rejectPoll = false;
  request = async path => {
    if (path.startsWith('chats?')) {
      polls++;
      if (rejectPoll) throw new Error('Offline fixture');
      return { items: listed };
    }
    return structuredClone(page);
  };
  try {
    $('draft').value = 'Keep this draft';
    $('draft').dispatchEvent(new Event('input'));
    compose();
    $('transcript').scrollTop = 150;
    await refreshApp();
    assert(conversations[0].id === 'new-chat' && conversations[selected] === active, 'Polling must discover chats without switching selection');
    assert($('draft').value === 'Keep this draft' && document.activeElement === $('draft'), 'Polling must preserve draft and focus');
    assert(Math.abs($('transcript').scrollTop - 150) < 2, 'Polling must preserve reading position');
    assert(document.querySelectorAll('[aria-current="true"]').length === 1, 'Polling must retain selected row');
    openSearch();
    $('search').value = 'New arrival';
    filterChats();
    listed.find(chat => chat.id === active.id).unreadCount = 0;
    await refreshApp();
    assert($('search').value === 'New arrival' && visible.length === 1, 'Polling must preserve search');
    assert(buttons[selected].querySelector('.unread').hidden, 'Polling must preserve the local read marker');
    closeSearch();
    Object.defineProperty(document, 'hidden', { configurable: true, value: true });
    const beforeHidden = polls;
    await refreshApp();
    assert(polls === beforeHidden, 'Hidden app must not poll');
    delete document.hidden;
    document.dispatchEvent(new Event('visibilitychange'));
    while (refreshing) await frame();
    assert(polls === beforeHidden + 1, 'Returning to the app must refresh immediately');
    rejectPoll = true;
    await refreshApp();
    assert(!refreshing && conversations[selected] === active, 'Failed polling must retain content and allow retry');
    rejectPoll = false;
    const beforeTimer = polls;
    await new Promise(resolve => setTimeout(resolve, 31000));
    assert(polls > beforeTimer, '30-second timer must refresh automatically');
  } finally {
    delete document.hidden;
    request = originalRequest;
    for (const chat of conversations) chat.stale = false;
  }

  // Metadata must not stretch short bubbles; long text must still wrap.
  const bubbleChat = conversations[3];
  bubbleChat.messages = [
    ['Test conversation 4', '3:26 PM', 'Stuff like that'],
    ['You', '3:27 PM', 'OK'],
    ['Test conversation 4', '3:28 PM', 'Long message text '.repeat(40)],
  ].map(message => [...message, { sentAt: '2026-09-08T15:26:00Z' }]);
  select(3);
  for (const article of $('messages').querySelectorAll('.message')) {
    const body = article.querySelector('p');
    const bounds = body.getBoundingClientRect();
    const parent = article.getBoundingClientRect();
    assert(Math.abs(article.classList.contains('me') ? bounds.right - parent.right : bounds.left - parent.left) < 1, 'Bubble must retain sender alignment');
    assert(body.scrollWidth <= body.clientWidth && bounds.width <= parent.width, 'Long bubbles must wrap without overflow');
    if (body.textContent.length < 20) {
      const range = document.createRange();
      range.selectNodeContents(body);
      assert(Math.abs(bounds.width - range.getBoundingClientRect().width - 20) < 1, 'Short bubbles must fit text plus padding, not metadata');
    }
  }

  // Same sender and calendar minute, never merely less than 60 seconds apart.
  const groupedChat = conversations[3];
  groupedChat.messages = [
    ['Alex', '12:30', 'First', { sentAt: '2026-09-08T12:30:00Z' }],
    ['Alex', '12:30', 'Second', { sentAt: '2026-09-08T12:30:59Z' }],
    ['Alex', '12:31', 'Next minute', { sentAt: '2026-09-08T12:31:00Z' }],
    ['Sam', '12:31', 'Different sender', { sentAt: '2026-09-08T12:31:01Z' }],
    ['You', '12:31', 'Outgoing one', { sentAt: '2026-09-08T12:31:02Z' }],
    ['You', '12:31', 'Outgoing two', { sentAt: '2026-09-08T12:31:59Z' }],
    ['You', '12:31', 'Next day', { sentAt: '2026-09-09T12:31:00Z' }],
  ];
  select(3);
  assert([...$('messages').querySelectorAll('.message')].map(node => node.classList.contains('grouped')).join() === 'true,false,false,false,true,false,false', 'Grouping must respect sender, minute and date');
  assert(getComputedStyle($('messages').querySelector('.message-head')).display === 'none', 'Grouped metadata must be hidden');
  groupedChat.messages.splice(1, 1);
  renderMessages(groupedChat);
  assert(!$('messages').querySelector('.message').classList.contains('grouped'), 'Cached group end must reset after removal');

  // Link detection preserves text and punctuation, never interpreting message HTML.
  const linkText = 'See (https://example.com/a_(b)), www.example.org/path?q=1&x=2.\n<script>alert(1)</script>';
  assert(messageLinks(linkText).map(link => link.label).join('|') === 'https://example.com/a_(b)|www.example.org/path?q=1&x=2', 'URL punctuation or www detection failed');
  assert(messageLinks('javascript:alert(1) ftp://host/a https://user:pass@host/a user@www.example.com').length === 0, 'Unsafe or non-URL text became links');
  const linkChat = conversations[4];
  linkChat.messages = [[linkChat.name, '12:30', linkText], ['You', '12:31', 'https://example.com/product']].map(message => [...message, { sentAt: '2026-09-08T12:30:00Z' }]);
  select(4);
  linkObserver.disconnect();
  const linkedBody = $('messages').querySelector('p');
  assert(linkedBody.textContent === linkText && !linkedBody.querySelector('script'), 'Linkification changed or interpreted message text');
  assert(linkedBody.querySelectorAll('a').length === 2, 'Every inline URL must be clickable');
  const cards = [...$('messages').querySelectorAll('.link-card')];
  assert(cards.length === 2 && !cards[1].parentNode.querySelector('p'), 'URL-only messages must show one card without duplicate text');
  assert([...$('messages').querySelectorAll('a')].every(a => a.target === '_blank' && a.rel.includes('noreferrer')), 'Links must preserve the app and omit referrers');
  request = async () => ({ title: '<b>Preview title</b>', description: 'Page description', image: `${location.origin}/favicon.png` });
  try {
    await loadLinkPreview(cards[0]);
    assert(cards[0].querySelector('.link-title').textContent === '<b>Preview title</b>' && !cards[0].querySelector('b'), 'Metadata must stay plain text');
    assert(cards[0].querySelector('img').referrerPolicy === 'no-referrer', 'Preview image must omit referrer');
    cards[0].querySelector('img').dispatchEvent(new Event('error'));
    assert(!cards[0].querySelector('img'), 'Broken preview image must disappear');
    request = async () => { throw new Error('Preview unavailable'); };
    await loadLinkPreview(cards[1]);
    assert(cards[1].querySelector('.link-domain').textContent === 'example.com', 'Failed preview must retain clickable domain card');
    select(0);
    select(4);
    assert($('messages').querySelector('.link-card') === cards[0], 'Preview DOM must survive chat switches');
  } finally { request = originalRequest; }
  select(3);

  // No real send requests; keep the mock until the delayed metadata refresh finishes.
  let accept;
  request = async (path, options) => options?.method === 'POST'
    ? new Promise(resolve => { accept = resolve; })
    : path === 'send-session' ? { session: 'fixture-session' } : { items: [] };
  try {
    $('draft').value = 'New message';
    $('draft').dispatchEvent(new Event('input'));
    compose();
    const sending = sendDraft();
    assert(document.activeElement === $('transcript'), 'Submit must return focus to transcript');
    document.activeElement.dispatchEvent(new KeyboardEvent('keydown', { key: 'j', bubbles: true }));
    const active = conversations[selected];
    assert(active !== groupedChat, 'j must switch chats while sending');
    await Promise.resolve();
    accept({ accepted: true });
    await sending;
    assert(conversations[0] === groupedChat && $('chats').firstElementChild === buttons[0], 'Accepted send must move chat to top');
    assert(conversations[selected] === active && buttons[selected].getAttribute('aria-current') === 'true', 'Reordering must preserve active chat');
    buttons[0].click();
    assert(conversations[selected] === groupedChat, 'Reordered row must open correct chat');
    document.activeElement.dispatchEvent(new KeyboardEvent('keydown', { key: 'j', bubbles: true }));
    document.activeElement.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', bubbles: true }));
    assert(conversations[selected] === groupedChat, 'j/k must follow reordered sidebar');
    await new Promise(resolve => setTimeout(resolve, 850));
    request = async () => { throw new Error('Fixture rejection'); };
    select(1);
    const failedChat = conversations[selected];
    $('draft').value = 'Retain failed draft';
    $('draft').dispatchEvent(new Event('input'));
    await sendDraft();
    assert(conversations[1] === failedChat && $('draft').value === 'Retain failed draft', 'Failed send must retain draft without reordering');
  } finally { request = originalRequest; }

  $('draft').value = 'Multiple lines\n'.repeat(12);
  updateComposer();
  assert($('draft').style.height === '120px' && $('draft').style.overflowY === 'auto', 'Long drafts must cap height and scroll');
  $('draft').value = '';
  updateComposer();
  assert($('draft').style.height === '40px' && $('draft').style.overflowY === 'hidden', 'Empty draft must shrink');
  select(1);
  samples.sort((a, b) => a - b);
  return { result: 'PASS', switches: samples.length, medianMs: samples[30], p95Ms: samples[57], maxMs: samples[59] };
})()
