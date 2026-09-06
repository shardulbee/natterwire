package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/widgets/textinput"
)

type mode int

const (
	sidebar mode = iota
	insert
)

type conversation struct {
	page
	layout
	draft         *textinput.Model
	name, failure string
	loaded        bool
}

type app struct {
	base                          string
	demo, busy                    bool
	chats                         page
	cache                         map[string]*conversation
	selected, sidebarTop          int
	opened, status, failure       string
	mode                          mode
	pendingChats, pendingMessages bool
	moreChats, olderMessages      bool
	responses                     chan response
}

func newApp(base string, demo bool) *app {
	return &app{base: base, demo: demo, cache: make(map[string]*conversation), pendingChats: true, responses: make(chan response, 1)}
}

func (a *app) current() *conversation { return a.cache[a.opened] }

func (a *app) activate(m mode) {
	if a.selected >= len(a.chats.Items) {
		return
	}
	c := a.chats.Items[a.selected]
	if a.cache[c.ID] == nil {
		a.cache[c.ID] = &conversation{name: c.DisplayName, draft: textinput.New(), layout: layout{dirty: true, following: true}}
	}
	if a.opened != c.ID {
		a.opened, a.status = c.ID, ""
		a.current().bottom()
		a.pendingMessages, a.olderMessages = true, false
	}
	a.mode = m
}

func (a *app) refresh() { a.pendingChats, a.pendingMessages = true, a.opened != "" }

// One in-flight request matches the serial Mac API. Repeated refreshes coalesce.
// The worker owns HTTP and JSON parsing; only the event loop mutates application state.
func (a *app) pump(ctx context.Context, client *http.Client) {
	if a.busy {
		return
	}
	r := request{}
	if c := a.current(); c != nil && (a.pendingMessages || a.olderMessages) {
		r.chat = a.opened
		if a.olderMessages && !a.pendingMessages {
			r.before = c.NextBefore
		} else if len(c.Items) > 0 {
			r.head, r.date = c.Items[0].ID, c.Items[0].SentAt
		}
		a.pendingMessages, a.olderMessages = false, false
	} else if a.pendingChats || a.moreChats {
		if a.moreChats && !a.pendingChats {
			r.before = a.chats.NextBefore
		}
		a.pendingChats, a.moreChats = false, false
	} else {
		return
	}
	a.busy = true
	go func() {
		p, err := load(ctx, client, a.base, r, a.demo)
		select {
		case a.responses <- response{r, p, err}:
		case <-ctx.Done():
		}
	}()
}

func (a *app) accept(r response) {
	a.busy = false
	failure := ""
	if r.err != nil {
		failure = "Refresh failed. Cached data retained. Check API/Tailscale; r retries."
	}
	if r.chat != "" {
		c := a.cache[r.chat]
		if c == nil {
			return
		}
		c.failure = failure
		if failure == "" {
			c.dirty = c.page.merge(r.page, r.before != "") || c.dirty
			c.loaded = true
		}
		return
	}
	a.failure = failure
	if failure != "" {
		return
	}
	previous := ""
	if a.selected < len(a.chats.Items) {
		previous = a.chats.Items[a.selected].ID
	}
	a.chats.merge(r.page, r.before != "")
	a.selected = 0
	for i, c := range a.chats.Items {
		if c.ID == previous {
			a.selected = i
		}
		if cached := a.cache[c.ID]; cached != nil {
			cached.name = c.DisplayName
		}
	}
	if a.mode == sidebar || a.opened == "" {
		a.activate(a.mode)
	}
}

func (a *app) key(k vaxis.Key) bool {
	if k.EventType == vaxis.EventRelease {
		return true
	}
	c := a.current()
	if k.EventType == vaxis.EventPaste {
		if a.mode == insert && c != nil {
			k.Text = clean(k.Text, false)
			if k.Matches(vaxis.KeyEnter) || k.Matches(vaxis.KeyTab) || k.Matches('j', vaxis.ModCtrl) {
				k.Text = " "
			}
			c.draft.Update(k)
		}
		return true
	}
	if k.Matches('c', vaxis.ModCtrl) {
		return false
	}
	s := k.String()
	for _, key := range "GJK" {
		if k.Matches(key) {
			s = string(key)
		}
	}
	if a.mode == insert {
		switch s {
		case "Escape":
			a.mode = sidebar
		case "Enter":
			a.status = "Not sent: the API has no send endpoint. Your draft is unchanged."
		default:
			c.draft.Update(k)
		}
		return true
	}
	if k.Matches('d', vaxis.ModCtrl) {
		s = "Page_Down"
	} else if k.Matches('u', vaxis.ModCtrl) {
		s = "Page_Up"
	}
	switch s {
	case "q":
		return false
	case "r":
		a.refresh()
	case "i":
		if c != nil {
			a.mode = insert
		}
	case "J", "Down", "K", "Up":
		previous := a.selected
		if s == "J" || s == "Down" {
			a.selected = min(a.selected+1, max(0, len(a.chats.Items)-1))
		} else {
			a.selected = max(0, a.selected-1)
		}
		if previous != a.selected {
			a.activate(sidebar)
		}
	case "n":
		a.moreChats = a.chats.NextBefore != ""
	default:
		if c != nil {
			switch s {
			case "j":
				c.scroll(1)
			case "k":
				c.scroll(-1)
			case "Page_Down":
				c.scroll(max(1, c.height/2))
			case "Page_Up":
				c.scroll(-max(1, c.height/2))
			case "G", "End":
				c.bottom()
			case "o":
				a.olderMessages = c.NextBefore != ""
				if !a.olderMessages {
					a.status = "All available history is loaded."
				}
			}
		}
	}
	return true
}

func (a *app) navigation(k vaxis.Key) bool {
	if a.mode == insert || k.EventType == vaxis.EventPaste || k.EventType == vaxis.EventRelease {
		return false
	}
	if k.Matches('J') || k.Matches('K') || k.Matches('d', vaxis.ModCtrl) || k.Matches('u', vaxis.ModCtrl) {
		return true
	}
	switch k.String() {
	case "j", "k", "Up", "Down", "Page_Up", "Page_Down":
		return true
	}
	return false
}

func run(base string, demo bool) error {
	vx, err := vaxis.New(vaxis.Options{DisableMouse: true})
	if err != nil {
		return err
	}
	defer vx.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	a := newApp(base, demo)
	width := terminalWidth(vx)
	timer := time.NewTicker(30 * time.Second)
	defer timer.Stop()
	var pending vaxis.Event
	for {
		a.pump(ctx, client)
		a.draw(vx, width)
		vx.Render()
		event := pending
		pending = nil
		if event == nil {
			select {
			case event = <-vx.Events():
			case event = <-a.responses:
			case <-timer.C:
				a.refresh()
				continue
			}
		}
		switch e := event.(type) {
		case vaxis.QuitEvent:
			return nil
		case vaxis.Resize:
			vx.Resize(e)
		case vaxis.SyncFunc:
			e()
		case response:
			a.accept(e)
		case vaxis.PasteEndEvent:
			if a.mode == insert {
				a.current().draft.Update(e)
			}
		case vaxis.Key:
			if e.EventType != vaxis.EventPaste && e.Matches('l', vaxis.ModCtrl) {
				vx.Refresh()
				continue
			}
			if !a.key(e) {
				return nil
			}
			// Collapse queued repeat frames without delaying isolated input or reordering actions.
			for i := 0; i < 31 && a.navigation(e); i++ {
				select {
				case next := <-vx.Events():
					if k, ok := next.(vaxis.Key); ok && a.navigation(k) {
						a.key(k)
						continue
					}
					pending = next
				default:
				}
				break
			}
		}
	}
}

func main() {
	base := os.Getenv("NATTERWIRE_URL")
	if base == "" {
		base = "http://127.0.0.1:8741"
	}
	flag.StringVar(&base, "url", base, "Mac API URL")
	demo := flag.Bool("demo", false, "use synthetic messages without a server")
	flag.Parse()
	if !validBase(base) || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "Usage: natterwire-tui [--url HTTP(S)_URL] [--demo]")
		os.Exit(2)
	}
	if err := run(base, *demo); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
