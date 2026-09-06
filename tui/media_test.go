package main

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestMediaSourceReuseAndVersion(t *testing.T) {
	var data bytes.Buffer
	png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 100, 80)))
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/attachments/a" || r.URL.Query().Get("version") == "" {
			t.Error("missing media identity/version")
		}
		w.Write(data.Bytes())
	}))
	defer server.Close()
	m := newMediaCache()
	k := imageKey{"a", "1", image.Pt(50, 40), image.Pt(10, 20)}
	first := m.load(context.Background(), server.Client(), server.URL, k)
	if first == nil || first.Bounds().Size() != k.size {
		t.Fatal("missing fitted image")
	}
	k.size = image.Pt(25, 20)
	if m.load(context.Background(), server.Client(), server.URL, k) == nil || requests.Load() != 1 {
		t.Fatal("resize downloaded cached source")
	}
	k.version = "2"
	if m.load(context.Background(), server.Client(), server.URL, k) == nil || requests.Load() != 2 {
		t.Fatal("version change reused stale pixels")
	}
}

func TestMediaWorkDoesNotBlockTextOrScroll(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/attachments/a" {
			close(started)
			<-release
			http.Error(w, "missing", 404)
			return
		}
		w.Write([]byte(`{"items":[{"id":"text","text":"Ready"}]}`))
	}))
	defer server.Close()
	defer close(release)
	m := newMediaCache()
	l := layout{dirty: true, following: true}
	items := []item{{ID: "1", Attachments: []attachment{{MediaID: "a", Width: 100, Height: 100}}}}
	l.prepare(items, 40, 10, unicodeWidth, image.Pt(10, 20))
	m.pump(context.Background(), server.Client(), server.URL, &l)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("media did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	p, err := getPage(ctx, server.Client(), server.URL)
	if err != nil || p.Items[0].Text != "Ready" {
		t.Fatalf("text blocked on media: %v", err)
	}
	for range 100 {
		l.scroll(-1)
		l.scroll(1)
		l.prepare(items, 40, 10, unicodeWidth, image.Pt(10, 20))
	}
	if !m.busy {
		t.Fatal("test must exercise scrolling while media is still blocked")
	}
}

func TestDisplayCacheBudgetAndLayoutReuse(t *testing.T) {
	m := newMediaCache()
	// No Kitty objects allocated here; cache eviction does not need a terminal.
	pixels := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
	for i := range 12 {
		m.accept(nil, mediaResult{imageKey{id: string(rune('a' + i))}, pixels})
	}
	if m.bytes > displayBudget || len(m.displays) != 8 {
		t.Fatal("display cache exceeded its memory budget")
	}
	if _, ok := m.displays[imageKey{id: "a"}]; ok {
		t.Fatal("oldest image not evicted")
	}
	l := layout{dirty: true, following: true}
	items := []item{{ID: "1", Text: "hello", Attachments: []attachment{{MediaID: "a", Width: 100, Height: 100}}}}
	l.prepare(items, 40, 10, unicodeWidth, image.Pt(10, 20))
	old := l.blocks["1"].rows
	l.dirty = true
	l.prepare(append([]item{{ID: "2", Text: "new"}}, items...), 40, 10, unicodeWidth, image.Pt(10, 20))
	if &l.blocks["1"].rows[0] != &old[0] {
		t.Fatal("unchanged message was laid out again")
	}
}

func BenchmarkPhotoTranscriptWarmScroll(b *testing.B) {
	l := layout{dirty: true, following: true}
	items := make([]item, 1000)
	for i := range items {
		items[i] = item{ID: string(rune(i + 1)), Text: "photo caption", Attachments: []attachment{{MediaID: "a", Width: 4032, Height: 3024}}}
	}
	l.prepare(items, 100, 50, unicodeWidth, image.Pt(10, 20))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		l.scroll(-1)
		l.prepare(items, 100, 50, unicodeWidth, image.Pt(10, 20))
	}
}
