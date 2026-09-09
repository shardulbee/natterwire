// Run with agent-browser eval "$(cat api/tests/unread.js)" on the fixture preview.
// Reload afterward. Uses fake messages only.
(async () => {
  const assert = (ok, message) => { if (!ok) throw new Error(message); };
  await chatsReady;
  const originalRequest = request;
  const id = 'unread-test';
  localStorage.removeItem(`read-through:${id}`);
  readThrough.delete(id);
  const message = rowID => ['Test', '12:00', 'Incoming test message', { id: rowID, rowID, sentAt: '2026-09-08T12:00:00Z' }];
  const chat = { id, name: 'Unread test', unreadCount: 1, latestIncomingRowID: '9007199254740993', messages: [message('9007199254740993')] };
  request = async () => ({ items: [] });
  try {
    conversations = [chat];
    selected = -1;
    buildChats();
    const dot = buttons[0].querySelector('.unread');
    assert(!dot.hidden, 'Initial Mac unread status must seed the dot');
    Object.defineProperty(document, 'hidden', { configurable: true, value: true });
    select(0);
    assert(!dot.hidden, 'Hidden documents must not acknowledge');
    delete document.hidden;
    Object.defineProperty(document, 'hasFocus', { configurable: true, value: () => false });
    acknowledgeChat();
    assert(!dot.hidden, 'Unfocused documents must not acknowledge');
    delete document.hasFocus;
    acknowledgeChat();
    assert(dot.hidden, 'Viewing latest must clear');
    readThrough.delete(id);
    chat.unreadCount = 99;
    updateChat(0);
    assert(dot.hidden, 'Stored cursor must survive cache reset and Mac unread flags');
    chat.latestIncomingRowID = '9007199254740994';
    updateChat(0);
    acknowledgeChat();
    assert(!dot.hidden, 'Unloaded arrival must stay unread, including adjacent large row IDs');
    chat.messages = Array.from({ length: 70 }, () => message('9007199254740993'));
    chat.messages.push(message('9007199254740994'));
    renderMessages(chat);
    $('transcript').scrollTop = 0;
    acknowledgeChat();
    assert(!dot.hidden, 'Scrolled-up reader must keep arrival unread');
    $('transcript').scrollTop = $('transcript').scrollHeight;
    acknowledgeChat();
    assert(dot.hidden, 'Reaching bottom must clear arrival');
    chat.messages = [message('9007199254740993')];
    renderMessages(chat);
    acknowledgeChat();
    assert(readCursor(chat) === 9007199254740994n, 'Stale transcript must not regress cursor');
    chat.messages = [message('9007199254740995')];
    chat.messages[0][3].isFromMe = true;
    renderMessages(chat);
    acknowledgeChat();
    assert(dot.hidden, 'Outgoing messages must not create unread');
    return { result: 'PASS', checks: 'bootstrap, focus, hidden page, persistence, exact row IDs, unloaded arrival, scroll, stale cursor, outgoing' };
  } finally {
    request = originalRequest;
    delete document.hidden;
    delete document.hasFocus;
    localStorage.removeItem(`read-through:${id}`);
    readThrough.delete(id);
  }
})()
