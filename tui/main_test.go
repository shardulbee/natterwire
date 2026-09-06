package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"go.rockorager.dev/vaxis"
)

func TestMerge(t *testing.T) {
	p := demoPage("alex")
	update := page{Items: []item{{ID: "4", Text: "new"}, {ID: "3", Text: "edited"}}, NextBefore: "cursor"}
	if !p.merge(update, false) || len(p.Items) != 4 || p.Items[1].Text != "edited" || p.Items[3].ID != "1" || p.NextBefore != "" {
		t.Fatalf("refresh lost history or edits: %+v", p)
	}
	ptr := &p.Items[0]
	if p.merge(update, false) || ptr != &p.Items[0] {
		t.Fatal("unchanged refresh replaced cached items")
	}
	if p.merge(page{Items: []item{{ID: "1"}}}, true) {
		t.Fatal("duplicate older page changed items")
	}
	if !p.merge(page{Items: []item{{ID: "0", Text: "old"}}}, true) || len(p.Items) != 5 {
		t.Fatal("older page was not appended")
	}
	if !p.merge(page{Items: []item{}}, false) || len(p.Items) != 0 {
		t.Fatal("empty replacement did not clear messages")
	}
}

func TestHTTPAndControls(t *testing.T) {
	for _, base := range []string{"file:///etc/passwd", "https://example.test?x=1", "https://user:password@example.test", "http://", "https://example.test#fragment"} {
		if validBase(base) {
			t.Errorf("accepted invalid base %q", base)
		}
	}
	if !validBase("https://example.test:8741/prefix") {
		t.Fatal("rejected valid base")
	}
	u, _ := url.Parse((request{chat: "a/b", before: "a+=?"}).url("https://example.test/"))
	if u.EscapedPath() != "/chats/a%2Fb/messages" || u.Query().Get("before") != "a+=?" {
		t.Fatal("opaque ID or cursor was not escaped", u)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/valid":
			fmt.Fprint(w, `{"items":[{"id":"1","text":"hello\u001b[2J\n世界 ☕","sender":"Alex\nname"}],"nextBefore":null}`)
		case "/invalid":
			fmt.Fprint(w, `{}`)
		case "/oversized":
			fmt.Fprint(w, strings.Repeat(" ", (64<<20)+1))
		default:
			http.Error(w, "offline", 503)
		}
	}))
	defer server.Close()
	p, err := getPage(context.Background(), server.Client(), server.URL+"/valid")
	if err != nil || p.Items[0].Text != "hello [2J\n世界 ☕" || p.Items[0].Sender != "Alex name" {
		t.Fatalf("unsafe or lost text: %+v %v", p, err)
	}
	for _, path := range []string{"/invalid", "/oversized", "/offline"} {
		if _, err := getPage(context.Background(), server.Client(), server.URL+path); err == nil {
			t.Errorf("accepted %s", path)
		}
	}
}

func TestPaginationMustAdvance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"items":[{"id":"2","text":"new"}],"nextBefore":"stuck"}`)
	}))
	defer server.Close()
	_, err := load(context.Background(), server.Client(), server.URL, request{chat: "alex", head: "1"}, false)
	if err == nil {
		t.Fatal("non-advancing cursor was accepted")
	}
}

func TestModesCacheAndStaleResponses(t *testing.T) {
	a := newApp("", true)
	a.accept(response{page: demoPage("")})
	a.accept(response{request: request{chat: "alex"}, page: demoPage("alex")})
	c := a.current()
	c.prepare(c.Items, 40, 5, unicodeWidth, &vaxis.Vaxis{})
	rows := &c.rows[0]
	a.accept(response{request: request{chat: "alex"}, page: demoPage("alex")})
	if c.dirty {
		t.Fatal("unchanged poll dirtied layout")
	}
	a.key(vaxis.Key{Keycode: 'j'})
	if a.opened != "weekend" || a.mode != sidebar || !a.pendingMessages {
		t.Fatal("sidebar did not activate chat")
	}
	a.key(vaxis.Key{Keycode: 'i'})
	a.key(vaxis.Key{Keycode: 'j', Text: "j"})
	a.key(vaxis.Key{Keycode: 'q', Text: "q", EventType: vaxis.EventPaste})
	a.key(vaxis.Key{Keycode: 'j', Modifiers: vaxis.ModCtrl, EventType: vaxis.EventPaste})
	a.current().draft.Update(vaxis.PasteEndEvent{})
	a.key(vaxis.Key{Keycode: vaxis.KeyEnter})
	if a.current().draft.String() != "jq " || !strings.HasPrefix(a.status, "Not sent:") {
		t.Fatal("draft or paste shortcuts changed")
	}
	a.key(vaxis.Key{Keycode: vaxis.KeyEsc})
	if a.mode != transcript {
		t.Fatal("escape did not exit insert")
	}
	a.key(vaxis.Key{Keycode: 'h'})
	a.key(vaxis.Key{Keycode: 'k'})
	c.prepare(c.Items, 40, 5, unicodeWidth, &vaxis.Vaxis{})
	if a.current() != c || &c.rows[0] != rows || !c.following {
		t.Fatal("warm switch lost cache or bottom position")
	}
	a.pendingMessages = false
	a.key(vaxis.Key{Keycode: 'l'})
	if a.pendingMessages {
		t.Fatal("focusing transcript triggered another fetch")
	}
	c.scroll(-2)
	a.key(vaxis.Key{Keycode: 'g', ShiftedCode: 'G', Modifiers: vaxis.ModShift})
	if !c.following {
		t.Fatal("shifted G did not follow bottom")
	}
	a.accept(response{request: request{chat: "weekend"}, err: errors.New("offline")})
	if a.current() != c || c.failure != "" || a.cache["weekend"].failure == "" {
		t.Fatal("stale response changed current chat")
	}
	a.refresh()
	a.refresh()
	if !a.pendingChats || !a.pendingMessages {
		t.Fatal("refresh flags not queued")
	}
}

func TestLayoutAnchorAndEmoji(t *testing.T) {
	var messages []item
	for i := 15; i > 0; i-- {
		messages = append(messages, item{ID: fmt.Sprint(i), Text: "1234567890❤️ 👩‍👩‍👧‍👦", Sender: "Contact Name"})
	}
	l := layout{dirty: true, following: true}
	l.prepare(messages, 12, 5, unicodeWidth, &vaxis.Vaxis{})
	if l.rows[1].text != "1234567890❤️" || !strings.HasPrefix(l.rows[0].text, "Contact Name") {
		t.Fatal("split emoji or lost sender name")
	}
	for _, r := range l.rows {
		if r.offset > 0 && unicodeWidth(r.text) > 12 {
			t.Fatalf("wrapped row too wide: %q", r.text)
		}
	}
	l.scroll(-7)
	anchor := l.rows[l.top]
	messages = append([]item{{ID: "16", Text: "new"}}, messages...)
	l.dirty = true
	l.prepare(messages, 12, 5, unicodeWidth, &vaxis.Vaxis{})
	if l.rows[l.top] != anchor || l.newMessages != 1 {
		t.Fatal("refresh moved reading position or lost count")
	}
	l.prepare(messages, 10, 5, unicodeWidth, &vaxis.Vaxis{})
	if l.rows[l.top].id != anchor.id || l.rows[l.top].offset > anchor.offset || l.newMessages != 1 {
		t.Fatal("resize lost anchor or counted twice")
	}
	l.bottom()
	if l.newMessages != 0 || !l.following {
		t.Fatal("bottom did not reset count")
	}
}

func TestWorkerCancellationAndCoalescing(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(stopped)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := newApp(server.URL, false)
	a.pump(ctx, server.Client())
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request not started")
	}
	a.refresh()
	a.pump(ctx, server.Client())
	if !a.busy || !a.pendingChats {
		t.Fatal("coalesced refresh was lost")
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("request survived cancellation")
	}
}

func BenchmarkWarmLayout(b *testing.B) {
	messages := slices.Repeat(demoPage("alex").Items, 1000)
	l := layout{dirty: true, following: true}
	l.prepare(messages, 100, 50, unicodeWidth, &vaxis.Vaxis{})
	b.ResetTimer()
	for b.Loop() {
		l.prepare(messages, 100, 50, unicodeWidth, &vaxis.Vaxis{})
	}
}
