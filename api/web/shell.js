const $ = id => document.getElementById(id);
const mobile = matchMedia('(max-width: 600px)');
const drafts = new Map();
const attempts = new Map();
let conversations = [];
let buttons = [];
let visible = [];
let selected = -1;
let sendSession = '';
const mediaQueue = [];
let mediaLoading = 0;
const timeFormatter = new Intl.DateTimeFormat([], { hour: 'numeric', minute: '2-digit' });
const weekdayFormatter = new Intl.DateTimeFormat([], { weekday: 'short' });
const dayFormatter = new Intl.DateTimeFormat([], { month: 'long', day: 'numeric' });
// Message tuples live with the chat cache; discarded pages release their DOM too.
const messageElements = new WeakMap();
const searchableMessages = new WeakMap();

function loadQueuedMedia() {
  while (mediaLoading < 2 && mediaQueue.length) {
    const item = mediaQueue.shift();
    if (!item.frame.isConnected) {
      item.image.removeAttribute('src');
      continue;
    }
    mediaLoading++;
    const done = loaded => {
      mediaLoading--;
      if (loaded) {
        item.frame.disabled = false;
        item.frame.onclick = () => { if (!suppressMediaClick) openImage(item.image); };
      } else if (!item.frame.isConnected && item.tries < 3) {
        item.image.removeAttribute('src');
      } else if (item.tries++ < 3) {
        setTimeout(() => { mediaQueue.push(item); loadQueuedMedia(); }, item.tries * 400);
      } else {
        if (item.pluginPayload) {
          const article = item.frame.closest('.message');
          item.frame.remove();
          if (!article.querySelector('p, .link-card, .attachment-frame, .attachment-file')) article.hidden = true;
        } else {
          const fallback = document.createElement('span');
          fallback.className = 'attachment-file';
          fallback.textContent = item.filename || 'Attachment unavailable';
          item.frame.replaceChildren(fallback);
        }
      }
      loadQueuedMedia();
    };
    item.image.onload = () => done(true);
    item.image.onerror = () => done(false);
    item.image.src = `${item.url}${item.tries ? `&retry=${item.tries}` : ''}`;
  }
}
const mediaObserver = new IntersectionObserver(entries => {
  for (const entry of entries) if (entry.isIntersecting) {
    mediaObserver.unobserve(entry.target);
    mediaQueue.push(entry.target.mediaItem);
    loadQueuedMedia();
  }
}, { root: $('transcript'), rootMargin: '500px' });

function openImage(image) {
  $('lightbox').sourceFrame = image.parentNode;
  $('lightbox-image').append(image);
  $('lightbox').showModal();
}

function formatTime(value) {
  if (!value) return '';
  const date = new Date(value), now = new Date();
  if (date.toDateString() === now.toDateString()) return timeFormatter.format(date);
  const yesterday = new Date(now); yesterday.setDate(now.getDate() - 1);
  if (date.toDateString() === yesterday.toDateString()) return 'Yesterday';
  return weekdayFormatter.format(date);
}
function dayLabel(value) {
  const date = new Date(value), now = new Date();
  if (date.toDateString() === now.toDateString()) return 'Today';
  return dayFormatter.format(date);
}
function messageTuple(message, chat) {
  const text = (message.text || '').replaceAll('\ufffc', '').trim();
  return [message.isFromMe ? 'You' : message.sender || chat.name, formatTime(message.sentAt), text, message];
}
function visibleAttachments(message) {
  return (message.attachments || []).filter(item => !item.filename?.toLowerCase().endsWith('.pluginpayloadattachment') || item.mediaID && item.version && item.version !== 'missing' && item.width > 0 && item.height > 0 && Math.max(item.width, item.height) >= 256);
}
function previewText(message, chat) {
  const [, , text, raw] = messageTuple(message, chat);
  const sender = raw.isFromMe ? 'You: ' : raw.sender && raw.sender !== chat.name ? `${raw.sender}: ` : '';
  const attachments = visibleAttachments(raw);
  if (text) return sender + text;
  if (!attachments.length) return sender;
  const author = raw.isFromMe ? 'You' : raw.sender || chat.name;
  return `${author} sent ${attachments.some(item => item.mimeType?.startsWith('image/') || item.width > 0 && item.height > 0) ? 'an image' : 'an attachment'}`;
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
  const unread = button.querySelector('.unread');
  unread.hidden = !(chat.unreadCount > 0);
  button.setAttribute('aria-label', chat.name + (chat.unreadCount > 0 ? ', unread in Mac Messages' : ''));
}
async function loadPreview(index) {
  const chat = conversations[index];
  if (chat.previewLoaded || chat.messages && !chat.stale) return;
  chat.previewLoaded = true;
  try {
    const page = await request(`chats/${encodeURIComponent(chat.id)}/messages?limit=1&media=metadata`);
    if (page.items[0]) chat.preview = previewText(page.items[0], chat);
    updateChat(conversations.indexOf(chat));
  } catch {
    chat.preview = 'Preview unavailable';
    updateChat(conversations.indexOf(chat));
  }
}
function buildChats() {
  previewObserver.disconnect();
  $('chats').replaceChildren();
  buttons = conversations.map(chat => {
    const button = document.createElement('button');
    button.className = 'chat';
    button.setAttribute('aria-label', chat.name);
    button.title = chat.name;
    button.innerHTML = '<span class="unread" title="Unread in Mac Messages" aria-hidden="true" hidden></span><span class="chat-summary"><span class="chat-top"><span class="chat-name"></span><time></time></span><span class="preview"></span></span>';
    button.querySelector('.chat-name').textContent = chat.name;
    button.onclick = () => openChat(conversations.indexOf(chat));
    $('chats').append(button);
    return button;
  });
  visible = conversations.map((_, index) => index);
  for (const [index, button] of buttons.entries()) {
    updateChat(index);
    previewObserver.observe(button);
  }
}
const previewObserver = new IntersectionObserver(entries => {
  for (const entry of entries) if (entry.isIntersecting) {
    previewObserver.unobserve(entry.target);
    const index = buttons.indexOf(entry.target);
    if (index >= 0) loadPreview(index);
  }
});
async function loadChats() {
  $('count').hidden = false;
  $('count').textContent = 'Loading conversations…';
  try {
    const page = await request('chats?limit=100&sort=latest');
    conversations = page.items.map(chat => ({ id: chat.id, name: chat.displayName, unreadCount: chat.unreadCount, preview: '', time: formatTime(chat.lastMessageAt), lastMessageAt: chat.lastMessageAt, messages: null }));
    buildChats();
    $('count').hidden = conversations.length > 0;
    $('count').textContent = conversations.length ? '' : 'No conversations';
    if (conversations.length) select(0, false);
  } catch (error) {
    $('count').textContent = error.message;
  }
}
let refreshing = false;
async function refreshApp() {
  if (document.hidden || refreshing) return;
  refreshing = true;
  try {
    const page = await request('chats?limit=100&sort=latest');
    const active = conversations[selected];
    const cached = new Map(conversations.map(chat => [chat.id, chat]));
    const next = page.items.map(item => {
      const chat = cached.get(item.id) || { id: item.id, preview: '', messages: null };
      if (chat.lastMessageAt !== item.lastMessageAt) {
        chat.previewLoaded = false;
        // Inactive transcripts refresh when opened; previews need only the newest message.
        chat.stale = true;
      }
      Object.assign(chat, { name: item.displayName, unreadCount: item.unreadCount, time: formatTime(item.lastMessageAt), lastMessageAt: item.lastMessageAt });
      return chat;
    });
    // Keep open conversations and pending sends even outside the newest 100.
    for (const chat of conversations) {
      if ((chat === active || attempts.has(chat.id)) && !next.includes(chat)) next.push(chat);
    }
    const rebuild = next.length !== conversations.length || next.some((chat, i) => chat !== conversations[i] || buttons[i]?.title !== chat.name);
    conversations = next;
    selected = active ? conversations.indexOf(active) : -1;
    if (rebuild) {
      const focusedChat = buttons.indexOf(document.activeElement);
      const focusedID = focusedChat >= 0 ? [...cached.values()][focusedChat]?.id : null;
      const scrollTop = $('chats').scrollTop;
      buildChats();
      $('chats').scrollTop = scrollTop;
      if (focusedID) buttons[conversations.findIndex(chat => chat.id === focusedID)]?.focus({ preventScroll: true });
    }
    conversations.forEach((chat, index) => {
      updateChat(index);
      if (!chat.previewLoaded && chat !== active) previewObserver.observe(buttons[index]);
    });
    filterChats();
    if (active) {
      $('name').textContent = active.name;
      if (!mobile.matches || document.body.classList.contains('chat-open')) buttons[selected].setAttribute('aria-current', 'true');
      await loadMessages(active, true);
    } else if (conversations.length) select(0, false);
  } catch {
    // Keep cached content and drafts on transient failures; retry on the next tick.
  } finally { refreshing = false; }
}
function loadMessages(chat, refresh = false) {
  if (chat.loading) return chat.loading;
  if (chat.messages && !refresh) return Promise.resolve();
  chat.loading = fetchMessages(chat, refresh).finally(() => { chat.loading = null; });
  return chat.loading;
}
async function fetchMessages(chat, refresh) {
  chat.previewLoaded = true;
  const page = await request(`chats/${encodeURIComponent(chat.id)}/messages?limit=100&media=metadata`);
  const transcript = $('transcript');
  // Use the position at response time, not where the reader was when the request started.
  const preserveScroll = refresh && conversations[selected] === chat && transcript.scrollHeight - transcript.scrollTop - transcript.clientHeight > 2;
  const bottomOffset = transcript.scrollHeight - transcript.scrollTop;
  const accepted = chat.messages?.filter(message => message[3]?.localAccepted) || [];
  const previous = new Map(chat.messages?.map(message => [message[3]?.id, message]));
  const messages = page.items.slice().reverse().map(message => {
    const tuple = messageTuple(message, chat), cached = previous.get(message.id);
    return cached && JSON.stringify(cached) === JSON.stringify(tuple) ? cached : tuple;
  });
  const matched = new Set();
  for (const local of accepted) {
    const sentAt = new Date(local[3].sentAt).getTime();
    const match = messages.find(message => !matched.has(message[3].id) && message[3].isFromMe && message[2] === local[2].trim() && Math.abs(new Date(message[3].sentAt).getTime() - sentAt) < 120000);
    if (match) matched.add(match[3].id);
    else messages.push(local);
  }
  messages.sort((a, b) => new Date(a[3]?.sentAt) - new Date(b[3]?.sentAt));
  const changed = messages.length !== chat.messages?.length || messages.some((message, index) => message !== chat.messages[index]);
  chat.messages = messages;
  chat.stale = false;
  if (page.items[0]) chat.preview = previewText(page.items[0], chat);
  updateChat(conversations.indexOf(chat));
  if (conversations[selected] === chat && changed) {
    renderMessages(chat);
    if (preserveScroll) transcript.scrollTop = transcript.scrollHeight - bottomOffset;
  }
}
function messageLinks(text) {
  const links = [];
  for (const match of text.matchAll(/\b(?:https?:\/\/|www\.)[^\s<>"\uFFFC]+/gi)) {
    if (match.index && /[\w@/]/.test(text[match.index - 1])) continue;
    let label = match[0].replace(/[.,!?:;'”’]+$/, '');
    for (const [open, close] of [['(', ')'], ['[', ']'], ['{', '}']]) {
      while (label.endsWith(close) && label.split(close).length > label.split(open).length) label = label.slice(0, -1);
    }
    try {
      const url = new URL(/^www\./i.test(label) ? `https://${label}` : label);
      if (!url.hostname || url.username || url.password) continue;
      links.push({ index: match.index, label, url });
    } catch {}
  }
  return links;
}
function externalLink(url) {
  const link = document.createElement('a');
  link.href = url;
  link.target = '_blank';
  link.rel = 'noopener noreferrer';
  return link;
}
const linkObserver = new IntersectionObserver(entries => {
  for (const entry of entries) if (entry.isIntersecting) {
    linkObserver.unobserve(entry.target);
    loadLinkPreview(entry.target);
  }
}, { root: $('transcript'), rootMargin: '100px' });
async function loadLinkPreview(card) {
  if (card.previewLoaded) return;
  card.previewLoaded = true;
  try {
    const metadata = await request(`link-preview?url=${encodeURIComponent(card.href)}`);
    const transcript = $('transcript');
    const atLatest = transcript.scrollHeight - transcript.scrollTop - transcript.clientHeight < 2;
    if (metadata.title) card.querySelector('.link-title').textContent = metadata.title;
    if (metadata.description) {
      const description = document.createElement('span');
      description.className = 'link-description';
      description.textContent = metadata.description;
      card.querySelector('.link-domain').before(description);
    }
    if (metadata.image && /^https?:\/\//i.test(metadata.image)) {
      const image = document.createElement('img');
      image.className = 'link-image';
      image.alt = '';
      image.referrerPolicy = 'no-referrer';
      image.decoding = 'async';
      // Reserve space before loading so the image cannot shift the transcript later.
      image.onerror = () => {
        const atLatest = transcript.scrollHeight - transcript.scrollTop - transcript.clientHeight < 2;
        image.remove();
        if (card.isConnected && atLatest) transcript.scrollTop = transcript.scrollHeight;
      };
      card.prepend(image);
      image.src = metadata.image;
    }
    if (card.isConnected && atLatest) transcript.scrollTop = transcript.scrollHeight;
  } catch {} // The domain/path card remains useful when previews are unavailable.
}
function appendMessage(fragment, chat, message, extraClass = '') {
  const cached = messageElements.get(message);
  if (cached) {
    fragment.append(cached);
    return cached;
  }
  const [sender, time, text] = message;
  const article = document.createElement('article');
  article.className = `message${sender === 'You' ? ' me' : ''}${extraClass}`;
  if (text) {
    const links = messageLinks(text);
    const body = document.createElement('p');
    let offset = 0;
    for (const { index, label, url } of links) {
      body.append(text.slice(offset, index));
      const link = externalLink(url.href);
      link.textContent = label;
      body.append(link);
      offset = index + label.length;
    }
    body.append(text.slice(offset));
    if (links.length !== 1 || text.trim() !== links[0].label) article.append(body);
    // One preview per message keeps multi-link messages compact; every URL remains clickable.
    if (links.length) {
      const { url } = links[0];
      const card = externalLink(url.href);
      card.className = 'link-card';
      card.title = url.href;
      const caption = document.createElement('span');
      caption.className = 'link-caption';
      const title = document.createElement('span');
      title.className = 'link-title';
      title.textContent = url.pathname === '/' ? url.hostname : url.pathname.slice(1) + url.search;
      const domain = document.createElement('span');
      domain.className = 'link-domain';
      domain.textContent = url.hostname;
      caption.append(title, domain);
      card.append(caption);
      article.append(card);
    }
  }
  const attachments = visibleAttachments(message[3] || {});
  if (attachments.length) {
    const media = document.createElement('div');
    media.className = 'attachments';
    for (const attachment of attachments) {
      const fallback = () => {
        const unavailable = document.createElement('span');
        unavailable.className = 'attachment-file';
        unavailable.textContent = attachment.filename || 'Attachment unavailable';
        return unavailable;
      };
      if (attachment.mediaID && attachment.version && attachment.version !== 'missing' && (attachment.mimeType?.startsWith('image/') || attachment.width && attachment.height)) {
        const frame = document.createElement('button');
        frame.type = 'button';
        frame.className = 'attachment-frame';
        frame.disabled = true;
        frame.setAttribute('aria-label', attachment.filename ? `Open image: ${attachment.filename}` : 'Open image');
        if (attachment.width && attachment.height) {
          const ratio = attachment.width / attachment.height;
          frame.style.aspectRatio = `${attachment.width} / ${attachment.height}`;
          frame.style.width = `min(420px, ${ratio * 55}vh, ${ratio * 520}px)`;
        }
        const image = document.createElement('img');
        image.className = 'attachment-image';
        image.alt = attachment.filename ? `Image: ${attachment.filename}` : 'Image attachment';
        image.decoding = 'async';
        image.draggable = false;
        if (attachment.width && attachment.height) {
          image.width = attachment.width;
          image.height = attachment.height;
        }
        frame.append(image);
        frame.mediaItem = { frame, image, filename:attachment.filename, pluginPayload:attachment.filename?.toLowerCase().endsWith('.pluginpayloadattachment'), url:`attachments/${encodeURIComponent(attachment.mediaID)}?version=${encodeURIComponent(attachment.version)}`, tries:0 };
        media.append(frame);
      } else media.append(fallback());
    }
    article.append(media);
  }
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
  article.append(head);
  fragment.append(article);
  messageElements.set(message, article);
  return article;
}
function renderMessages(chat, query = $('search').value.trim().toLowerCase()) {
  if (conversations[selected] !== chat) return;
  mediaObserver.disconnect();
  linkObserver.disconnect();
  for (const item of mediaQueue) item.image.removeAttribute('src');
  mediaQueue.length = 0;
  if (!chat.messages) {
    const loading = document.createElement('div');
    loading.className = 'date';
    loading.textContent = 'Loading messages…';
    $('messages').replaceChildren(loading);
    return;
  }
  const fragment = document.createDocumentFragment();
  let day = '', match, displayed = 0, previous, previousArticle;
  for (const message of chat.messages) {
    if (!message[2] && !visibleAttachments(message[3] || {}).length) continue;
    displayed++;
    const nextDay = dayLabel(message[3]?.sentAt);
    if (nextDay !== day) {
      const date = document.createElement('div');
      date.className = 'date';
      date.textContent = nextDay;
      fragment.append(date);
      day = nextDay;
    }
    const article = appendMessage(fragment, chat, message, message[3]?.localAccepted ? ' accepted' : '');
    article.classList.remove('grouped');
    if (previous && previous[0] === message[0] &&
        Math.floor(Date.parse(previous[3]?.sentAt) / 60000) === Math.floor(Date.parse(message[3]?.sentAt) / 60000)) {
      previousArticle.classList.add('grouped');
    }
    previous = message;
    previousArticle = article;
    if (query && !match && message[2].toLowerCase().includes(query)) match = article;
  }
  const attempt = attempts.get(chat.id);
  if (attempt && (attempt.state !== 'failed' || drafts.get(chat.id) !== attempt.text)) {
    const state = attempt.state === 'sending' ? 'Sending…' : 'Not confirmed';
    appendMessage(fragment, chat, ['You', state, attempt.text], attempt.state === 'failed' ? ' failed' : ' pending');
  }
  if (!displayed && !attempt) {
    const empty = document.createElement('div');
    empty.className = 'date';
    empty.textContent = 'No messages';
    fragment.append(empty);
  }
  $('messages').replaceChildren(fragment);
  for (const card of $('messages').querySelectorAll('.link-card')) {
    if (!card.previewLoaded) linkObserver.observe(card);
  }
  for (const frame of $('messages').querySelectorAll('.attachment-frame')) {
    if (!frame.mediaItem.image.hasAttribute('src')) mediaObserver.observe(frame);
  }
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
  const height = draft.scrollHeight;
  draft.style.height = `${Math.max(40, Math.min(120, height))}px`;
  draft.style.overflowY = height > 120 ? 'auto' : 'hidden';
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
  $('transcript').focus({ preventScroll: true });
  updateComposer();
  renderMessages(chat);
  $('status').textContent = '';
  try {
    if (!attempt.key) {
      if (!sendSession) sendSession = (await request('send-session')).session;
      if (!sendSession) throw new Error('Missing send session');
      attempt.key = `${sendSession}:${crypto.randomUUID()}`;
    }
    const receipt = await request(`chats/${encodeURIComponent(chat.id)}/messages`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Idempotency-Key': attempt.key },
      body: JSON.stringify({ text: attempt.text }),
    });
    if (!receipt.accepted) throw new Error('Missing acceptance receipt');
    $('status').textContent = '';
    chat.messages.push(['You', formatTime(attempt.sentAt), attempt.text, { isFromMe: true, sentAt: attempt.sentAt, localAccepted: true }]);
    chat.preview = `You: ${attempt.text}`;
    chat.time = formatTime(attempt.sentAt);
    const active = conversations[selected], index = conversations.indexOf(chat);
    conversations.unshift(...conversations.splice(index, 1));
    buttons.unshift(...buttons.splice(index, 1));
    $('chats').prepend(buttons[0]);
    selected = conversations.indexOf(active);
    updateChat(0);
    filterChats();
    attempts.delete(chat.id);
    renderMessages(chat);
    setTimeout(() => loadMessages(chat, true).catch(() => {}), 750);
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
  if (!mobile.matches || open) buttons[index].setAttribute('aria-current', 'true');
  const chat = conversations[index];
  $('name').textContent = chat.name;
  $('draft').value = drafts.get(chat.id) || '';
  updateComposer();
  browse();
  renderMessages(chat, query);
  $('status').textContent = '';
  if (!chat.messages || chat.stale) loadMessages(chat, true).catch(error => { if (conversations[selected] === chat) $('status').textContent = error.message; });
  if (open && mobile.matches) $('name').focus({ preventScroll: true });
}
function openChat(index) {
  if (!mobile.matches || document.body.classList.contains('chat-open')) return select(index);
  select(index, false);
  buttons[index].setAttribute('aria-current', 'true');
  history.pushState({ ...history.state, natterwireChat: true }, '');
  document.body.classList.add('chat-open');
  $('name').focus({ preventScroll: true });
}
function compose() { $('draft').focus(); }
function showIndex() {
  $('draft').blur();
  document.body.classList.remove('chat-open');
  if (mobile.matches) buttons[selected]?.removeAttribute('aria-current');
  buttons[selected]?.focus({ preventScroll: true });
}
let appHistoryNavigation = false;
function closeChat() {
  if (history.state?.natterwireChat) {
    appHistoryNavigation = true;
    history.back();
  }
  else showIndex();
}
$('back').onclick = closeChat;
window.addEventListener('popstate', () => {
  const nativeNavigation = !appHistoryNavigation;
  appHistoryNavigation = false;
  if (nativeNavigation) document.documentElement.classList.add('native-history');
  if (history.state?.natterwireChat) document.body.classList.add('chat-open');
  else showIndex();
  if (nativeNavigation) requestAnimationFrame(() => requestAnimationFrame(() => document.documentElement.classList.remove('native-history')));
});
let swipe, suppressMediaClick = false;
$('transcript').onpointerdown = event => {
  if (mobile.matches && event.pointerType === 'touch' && event.isPrimary) {
    swipe = { x: event.clientX, y: event.clientY };
    if (!event.target.closest('a')) $('transcript').setPointerCapture(event.pointerId);
  }
};
$('transcript').addEventListener('click', event => {
  if (suppressMediaClick && event.target.closest('a')) event.preventDefault();
}, true);
$('transcript').onpointerup = event => {
  if (swipe) {
    const dx = event.clientX - swipe.x, dy = event.clientY - swipe.y;
    if (dx > 56 && Math.abs(dy) < 56 && dx > Math.abs(dy) * 1.5) {
      suppressMediaClick = true;
      event.preventDefault();
      closeChat();
      setTimeout(() => { suppressMediaClick = false; });
    }
  }
  swipe = null;
};
$('transcript').onpointercancel = () => { swipe = null; };
let trackpadX = 0, trackpadTimer;
$('app').addEventListener('wheel', event => {
  if (!mobile.matches || !document.body.classList.contains('chat-open') || Math.abs(event.deltaX) <= Math.abs(event.deltaY)) return;
  event.preventDefault();
  trackpadX += event.deltaX;
  clearTimeout(trackpadTimer);
  if (Math.abs(trackpadX) > 80) {
    trackpadX = 0;
    closeChat();
  }
  trackpadTimer = setTimeout(() => { trackpadX = 0; }, 200);
}, { passive: false });
$('draft').oninput = () => { if (selected >= 0) drafts.set(conversations[selected].id, $('draft').value); updateComposer(); };
$('send').onclick = sendDraft;
$('close-help').onclick = () => $('shortcuts').close();
$('close-lightbox').onclick = () => $('lightbox').close();
$('lightbox').onclick = event => { if (event.target === $('lightbox')) $('lightbox').close(); };
$('lightbox').onclose = () => {
  const frame = $('lightbox').sourceFrame;
  if (frame) frame.append($('lightbox-image').firstElementChild);
  $('lightbox').sourceFrame = null;
};
function openSearch() {
  if (mobile.matches) closeChat();
  $('search-panel').classList.add('is-open');
  if (!mobile.matches) $('brand').setAttribute('aria-hidden', 'true');
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
  for (const [index, button] of buttons.entries()) {
    const chat = conversations[index];
    const message = query && chat.messages?.find(message => {
      if (!searchableMessages.has(message)) searchableMessages.set(message, message[2].toLowerCase());
      return searchableMessages.get(message).includes(query);
    });
    const match = chat.name.toLowerCase().includes(query) || chat.preview.toLowerCase().includes(query) || !!message;
    if (button.hidden === match) button.hidden = !match;
    const preview = button.querySelector('.preview');
    const text = message ? message[2].slice(Math.max(0, searchableMessages.get(message).indexOf(query) - 24)) : chat.preview || 'No message preview';
    if (preview.textContent !== text) preview.textContent = text;
    if (match) visible.push(index);
  }
  $('count').hidden = visible.length > 0;
  $('count').textContent = visible.length ? '' : 'No conversations found';
}
document.addEventListener('keydown', event => {
  if (event.isComposing || $('shortcuts').open || $('lightbox').open) return;
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
      if (event.key === 'j' || position > 0) openChat(visible[(position + (event.key === 'j' ? 1 : -1) + visible.length) % visible.length]);
      buttons[selected].scrollIntoView({ block: 'nearest' });
    }
  } else if (event.key === 'G') $('transcript').scrollTop = $('transcript').scrollHeight;
  else if (event.key === 'r') {
    const chat = conversations[selected];
    if (chat) {
      $('status').textContent = 'Refreshing…';
      loadMessages(chat, true).then(() => {
        if (conversations[selected] === chat) $('status').textContent = '';
      }).catch(error => { if (conversations[selected] === chat) $('status').textContent = error.message; });
    }
  }
  else if (event.key === 'i') compose();
  else if (event.key === '/') openSearch();
  else if (event.key === '?') $('shortcuts').showModal();
  else return;
  event.preventDefault();
});

const chatsReady = loadChats().then(() => {
  // Hidden PWAs may be suspended by the OS. Catch up as soon as they return.
  setInterval(refreshApp, 30000);
  document.addEventListener('visibilitychange', refreshApp);
  window.addEventListener('focus', refreshApp);
  window.addEventListener('online', refreshApp);
});

async function openNotificationChat(id) {
  if (typeof id !== 'string' || !/^[A-Za-z0-9_-]{1,2048}$/.test(id)) return;
  await chatsReady;
  try {
    let index = conversations.findIndex(chat => chat.id === id), before;
    // An older notification may target a chat outside the first page.
    while (index < 0) {
      const page = await request(`chats?limit=100&sort=latest${before ? `&before=${encodeURIComponent(before)}` : ''}`);
      const item = page.items.find(chat => chat.id === id);
      if (item) {
        index = conversations.push({ id: item.id, name: item.displayName, unreadCount: item.unreadCount, preview: '', time: formatTime(item.lastMessageAt), messages: null }) - 1;
        buildChats();
        break;
      }
      before = page.nextBefore;
      if (!before) throw new Error('This notification’s conversation is no longer available');
    }
    openChat(index);
    await loadMessages(conversations[index], true);
  } catch (error) { $('status').textContent = error.message; }
}
const notificationChat = new URL(location.href).searchParams.get('chat');
if (notificationChat) {
  openNotificationChat(notificationChat);
  history.replaceState(history.state, '', location.pathname);
}
