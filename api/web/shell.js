const $ = id => document.getElementById(id);
const mobile = matchMedia('(max-width: 600px)');
const drafts = new Map();
const attempts = new Map();
let conversations = [];
let buttons = [];
let visible = [];
let selected = -1;
let sendSession = '';

function formatTime(value) {
  if (!value) return '';
  const date = new Date(value), now = new Date();
  if (date.toDateString() === now.toDateString()) return new Intl.DateTimeFormat([], { hour: 'numeric', minute: '2-digit' }).format(date);
  const yesterday = new Date(now); yesterday.setDate(now.getDate() - 1);
  if (date.toDateString() === yesterday.toDateString()) return 'Yesterday';
  return new Intl.DateTimeFormat([], { weekday: 'short' }).format(date);
}
function dayLabel(value) {
  const date = new Date(value), now = new Date();
  if (date.toDateString() === now.toDateString()) return 'Today';
  return new Intl.DateTimeFormat([], { month: 'long', day: 'numeric' }).format(date);
}
function messageTuple(message, chat) {
  const text = message.text || (message.attachments?.length ? '[Attachment]' : '');
  return [message.isFromMe ? 'You' : message.sender || chat.name, formatTime(message.sentAt), text, message];
}
function previewText(message, chat) {
  const [, , text, raw] = messageTuple(message, chat);
  const sender = raw.isFromMe ? 'You: ' : raw.sender && raw.sender !== chat.name ? `${raw.sender}: ` : '';
  return sender + text;
}
async function request(path, options) {
  const response = await fetch(path, options);
  let body = {};
  try { body = await response.json(); } catch {}
  if (!response.ok) throw new Error(body.error || `Request failed (${response.status})`);
  return body;
}
function updateChat(index) {
  const button = buttons[index], chat = conversations[index];
  if (!button) return;
  button.querySelector('.preview').textContent = chat.preview || 'No message preview';
  button.querySelector('time').textContent = chat.time;
}
async function loadPreview(index) {
  const chat = conversations[index];
  if (chat.previewLoaded || chat.messages) return;
  chat.previewLoaded = true;
  try {
    const page = await request(`/chats/${encodeURIComponent(chat.id)}/messages?limit=1&media=metadata`);
    if (page.items[0]) chat.preview = previewText(page.items[0], chat);
    updateChat(index);
  } catch {
    chat.preview = 'Preview unavailable';
    updateChat(index);
  }
}
function buildChats() {
  $('chats').replaceChildren();
  buttons = conversations.map((chat, index) => {
    const button = document.createElement('button');
    button.className = 'chat';
    button.setAttribute('aria-label', chat.name);
    button.title = chat.name;
    button.innerHTML = '<span class="chat-summary"><span class="chat-top"><span class="chat-name"></span><time></time></span><span class="preview"></span></span>';
    button.querySelector('.chat-name').textContent = chat.name;
    button.onclick = () => openChat(index);
    $('chats').append(button);
    updateChat(index);
    return button;
  });
  visible = conversations.map((_, index) => index);
  const previewObserver = new IntersectionObserver(entries => {
    for (const entry of entries) if (entry.isIntersecting) loadPreview(buttons.indexOf(entry.target));
  });
  for (const button of buttons) previewObserver.observe(button);
}
async function loadChats() {
  $('count').hidden = false;
  $('count').textContent = 'Loading conversations…';
  try {
    const page = await request('/chats?limit=100');
    conversations = page.items.map(chat => ({ id: chat.id, name: chat.displayName, preview: '', time: formatTime(chat.lastMessageAt), messages: null }));
    buildChats();
    $('count').hidden = conversations.length > 0;
    $('count').textContent = conversations.length ? '' : 'No conversations';
    if (conversations.length) select(0, false);
  } catch (error) {
    $('count').textContent = error.message;
  }
}
async function loadMessages(chat) {
  if (chat.messages) return;
  chat.previewLoaded = true;
  const page = await request(`/chats/${encodeURIComponent(chat.id)}/messages?limit=100&media=metadata`);
  chat.messages = page.items.slice().reverse().map(message => messageTuple(message, chat));
  if (page.items[0]) chat.preview = previewText(page.items[0], chat);
  updateChat(conversations.indexOf(chat));
  if (conversations[selected] === chat) renderMessages(chat);
}
function appendMessage(fragment, chat, message, extraClass = '') {
  const [sender, time, text] = message;
  const article = document.createElement('article');
  article.className = `message${sender === 'You' ? ' me' : ''}${extraClass}`;
  const body = document.createElement('p');
  body.textContent = text;
  const head = document.createElement('div');
  head.className = 'message-head';
  if (sender !== 'You' && sender !== chat.name) {
    const author = document.createElement('strong');
    author.textContent = sender;
    head.append(author);
  }
  const timestamp = document.createElement('time');
  timestamp.textContent = time;
  head.append(timestamp);
  article.append(body, head);
  fragment.append(article);
  return article;
}
function renderMessages(chat, query = $('search').value.trim().toLowerCase()) {
  if (!chat.messages) {
    const loading = document.createElement('div');
    loading.className = 'date';
    loading.textContent = 'Loading messages…';
    $('messages').replaceChildren(loading);
    return;
  }
  const fragment = document.createDocumentFragment();
  let day = '', match;
  for (const message of chat.messages) {
    const nextDay = dayLabel(message[3]?.sentAt);
    if (nextDay !== day) {
      const date = document.createElement('div');
      date.className = 'date';
      date.textContent = nextDay;
      fragment.append(date);
      day = nextDay;
    }
    const article = appendMessage(fragment, chat, message, message[3]?.localAccepted ? ' accepted' : '');
    if (query && !match && message[2].toLowerCase().includes(query)) match = article;
  }
  const attempt = attempts.get(chat.id);
  if (attempt && (attempt.state !== 'failed' || drafts.get(chat.id) !== attempt.text)) {
    const state = attempt.state === 'sending' ? 'Sending…' : 'Not confirmed';
    appendMessage(fragment, chat, ['You', state, attempt.text], attempt.state === 'failed' ? ' failed' : ' pending');
  }
  if (!chat.messages.length && !attempt) {
    const empty = document.createElement('div');
    empty.className = 'date';
    empty.textContent = 'No messages';
    fragment.append(empty);
  }
  $('messages').replaceChildren(fragment);
  $('transcript').scrollTop = $('transcript').scrollHeight;
  if (match) $('transcript').scrollTop += match.getBoundingClientRect().top - $('transcript').getBoundingClientRect().top - 12;
}
function browse() { $('draft').blur(); }
function updateComposer() {
  const draft = $('draft');
  const chat = conversations[selected];
  const sending = chat && attempts.get(chat.id)?.state === 'sending';
  const atLatest = $('transcript').scrollHeight - $('transcript').scrollTop - $('transcript').clientHeight < 2;
  draft.style.height = '40px';
  draft.style.height = `${Math.max(40, Math.min(120, draft.scrollHeight))}px`;
  draft.style.overflowY = draft.scrollHeight > 120 ? 'auto' : 'hidden';
  $('send').disabled = !!sending || !draft.value.trim();
  if (atLatest) requestAnimationFrame(() => $('transcript').scrollTop = $('transcript').scrollHeight);
}
async function sendDraft() {
  const chat = conversations[selected], text = $('draft').value;
  if (!chat || $('send').disabled) return;
  if (!text.trim() || new TextEncoder().encode(text).length > 16000 || text.includes('\0')) {
    $('status').textContent = 'Enter 1–16000 bytes of text';
    return;
  }
  let attempt = attempts.get(chat.id);
  if (attempt && attempt.text !== text) {
    $('status').textContent = 'Restore the unconfirmed draft before retrying';
    return;
  }
  if (!attempt) {
    attempt = { text, key: '', state: 'sending', sentAt: new Date().toISOString() };
    attempts.set(chat.id, attempt);
  } else attempt.state = 'sending';
  drafts.set(chat.id, '');
  $('draft').value = '';
  updateComposer();
  renderMessages(chat);
  $('status').textContent = '';
  try {
    if (!attempt.key) {
      if (!sendSession) sendSession = (await request('/send-session')).session;
      if (!sendSession) throw new Error('Missing send session');
      attempt.key = `${sendSession}:${crypto.randomUUID()}`;
    }
    const receipt = await request(`/chats/${encodeURIComponent(chat.id)}/messages`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Idempotency-Key': attempt.key },
      body: JSON.stringify({ text: attempt.text }),
    });
    if (!receipt.accepted) throw new Error('Missing acceptance receipt');
    $('status').textContent = 'Accepted by Messages · delivery unconfirmed';
    chat.messages.push(['You', 'Accepted', attempt.text, { isFromMe: true, sentAt: attempt.sentAt, localAccepted: true }]);
    chat.preview = `You: ${attempt.text}`;
    chat.time = formatTime(attempt.sentAt);
    updateChat(conversations.indexOf(chat));
    attempts.delete(chat.id);
    renderMessages(chat);
  } catch (error) {
    attempt.state = 'failed';
    if (!drafts.get(chat.id)) drafts.set(chat.id, attempt.text);
    if (conversations[selected] === chat) $('draft').value = drafts.get(chat.id);
    $('status').textContent = `Could not confirm send (${error.message}) · check Messages before retrying · request retained`;
    renderMessages(chat);
  } finally {
    if (conversations[selected] === chat) updateComposer();
  }
}
function select(index, open = true) {
  if (index < 0 || !conversations[index]) return;
  const query = $('search').value.trim().toLowerCase();
  if (!$('search').disabled) closeSearch(false);
  if (open) document.body.classList.add('chat-open');
  if (selected >= 0) buttons[selected].removeAttribute('aria-current');
  selected = index;
  buttons[index].setAttribute('aria-current', 'true');
  const chat = conversations[index];
  $('name').textContent = chat.name;
  $('draft').value = drafts.get(chat.id) || '';
  updateComposer();
  browse();
  renderMessages(chat, query);
  $('status').textContent = '';
  if (!chat.messages) loadMessages(chat).catch(error => { if (conversations[selected] === chat) $('status').textContent = error.message; });
  if (open && mobile.matches) $('name').focus({ preventScroll: true });
}
function openChat(index) {
  if (!mobile.matches || document.body.classList.contains('chat-open')) return select(index);
  select(index, false);
  document.body.classList.add('chat-open');
  $('name').focus({ preventScroll: true });
}
function compose() { $('draft').focus(); }
function showIndex() {
  $('draft').blur();
  document.body.classList.remove('chat-open');
  buttons[selected]?.focus({ preventScroll: true });
}
$('back').onclick = showIndex;
let swipe;
$('transcript').onpointerdown = event => {
  if (mobile.matches && event.pointerType === 'touch' && event.isPrimary) {
    swipe = { x: event.clientX, y: event.clientY };
    $('transcript').setPointerCapture(event.pointerId);
  }
};
$('transcript').onpointerup = event => {
  if (swipe) {
    const dx = event.clientX - swipe.x, dy = event.clientY - swipe.y;
    if (dx > 80 && Math.abs(dy) < 40 && dx > Math.abs(dy) * 2) showIndex();
  }
  swipe = null;
};
$('transcript').onpointercancel = () => { swipe = null; };
$('draft').oninput = () => { if (selected >= 0) drafts.set(conversations[selected].id, $('draft').value); updateComposer(); };
$('send').onclick = sendDraft;
$('close-help').onclick = () => $('shortcuts').close();
function openSearch() {
  if (mobile.matches) showIndex();
  $('search-panel').classList.add('is-open');
  $('brand').setAttribute('aria-hidden', 'true');
  for (const control of [$('search'), $('close-search')]) {
    control.disabled = false;
    control.removeAttribute('aria-hidden');
  }
  $('toggle-search').setAttribute('aria-expanded', 'true');
  $('search').focus({ preventScroll: true });
}
function closeSearch(restoreFocus = true) {
  $('search-panel').classList.remove('is-open');
  $('brand').removeAttribute('aria-hidden');
  for (const control of [$('search'), $('close-search')]) {
    control.disabled = true;
    control.setAttribute('aria-hidden', 'true');
  }
  $('toggle-search').setAttribute('aria-expanded', 'false');
  $('search').value = '';
  filterChats();
  if (restoreFocus) $('toggle-search').focus();
}
$('toggle-search').onclick = openSearch;
$('close-search').onclick = () => closeSearch();
$('search').oninput = filterChats;
function filterChats() {
  visible = [];
  const query = $('search').value.trim().toLowerCase();
  for (const button of $('chats').children) {
    const index = buttons.indexOf(button), chat = conversations[index];
    const message = query && chat.messages?.find(([, , text]) => text.toLowerCase().includes(query));
    const match = chat.name.toLowerCase().includes(query) || chat.preview.toLowerCase().includes(query) || !!message;
    button.hidden = !match;
    button.querySelector('.preview').textContent = message ? message[2].slice(Math.max(0, message[2].toLowerCase().indexOf(query) - 24)) : chat.preview || 'No message preview';
    if (match) visible.push(index);
  }
  $('count').hidden = visible.length > 0;
  $('count').textContent = visible.length ? '' : 'No conversations found';
}
document.addEventListener('keydown', event => {
  if (event.isComposing || $('shortcuts').open) return;
  const input = event.target.matches('input, textarea');
  if (event.key === 'Escape') {
    if (!$('search').disabled) closeSearch();
    else if (input) { event.target.blur(); browse(); $('transcript').focus(); }
    return;
  }
  if (input) {
    if (event.key === 'Enter' && event.target === $('search') && visible.length) {
      event.preventDefault(); openChat(visible[0]); $('search').blur();
      if (!mobile.matches) $('transcript').focus();
    } else if (event.key === 'Enter' && event.target === $('draft') && !event.shiftKey) {
      event.preventDefault(); sendDraft();
    }
    return;
  }
  if (event.metaKey || event.altKey) return;
  if (event.ctrlKey && !['d', 'u'].includes(event.key)) return;
  if (event.ctrlKey) $('transcript').scrollTop += (event.key === 'd' ? 1 : -1) * $('transcript').clientHeight / 2;
  else if (event.key === 'j' || event.key === 'k') {
    if (visible.length) {
      const position = visible.indexOf(selected);
      openChat(visible[(position + (event.key === 'j' ? 1 : -1) + visible.length) % visible.length]);
      buttons[selected].scrollIntoView({ block: 'nearest' });
    }
  } else if (event.key === 'G') $('transcript').scrollTop = $('transcript').scrollHeight;
  else if (event.key === 'i') compose();
  else if (event.key === '/') openSearch();
  else if (event.key === '?') $('shortcuts').showModal();
  else return;
  event.preventDefault();
});

loadChats();
