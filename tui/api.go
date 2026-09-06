package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"unicode"
)

// Both API lists use the same ID/cursor contract. Only presentation fields are needed.
type item struct {
	ID, DisplayName, Text, SentAt, Sender string
	IsFromMe                              bool
}

type page struct {
	Items      []item
	NextBefore string
}

// merge retains loaded history below the refreshed head, or appends an older page.
// An unchanged poll keeps the existing slice and its prepared transcript layout.
func (p *page) merge(next page, older bool) bool {
	if !older && next.NextBefore != "" && len(next.Items) <= len(p.Items) && slices.Equal(p.Items[:len(next.Items)], next.Items) {
		return false
	}
	index := make(map[string]int, len(p.Items))
	for i, m := range p.Items {
		index[m.ID] = i
	}
	if older {
		count := len(p.Items)
		for _, m := range next.Items {
			if _, exists := index[m.ID]; !exists {
				index[m.ID] = len(p.Items)
				p.Items = append(p.Items, m)
			}
		}
		p.NextBefore = next.NextBefore
		return len(p.Items) != count
	}
	tail := len(p.Items)
	for _, m := range next.Items {
		if i, exists := index[m.ID]; exists {
			tail = i + 1
		}
	}
	if next.NextBefore != "" && tail < len(p.Items) {
		next.Items = append(next.Items, p.Items[tail:]...)
		next.NextBefore = p.NextBefore
	}
	changed := !slices.Equal(p.Items, next.Items)
	if changed {
		p.Items = next.Items
	}
	p.NextBefore = next.NextBefore
	return changed
}

type request struct{ chat, before, head, date string }
type response struct {
	request
	page
	err error
}

func validBase(base string) bool {
	u, err := url.Parse(base)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" &&
		u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == ""
}

func (r request) url(base string) string {
	path := strings.TrimRight(base, "/") + "/chats"
	if r.chat != "" {
		path += "/" + url.PathEscape(r.chat) + "/messages"
	}
	q := url.Values{"limit": {"50"}}
	if r.before != "" {
		q.Set("before", r.before)
	}
	return path + "?" + q.Encode()
}

func clean(s string, multiline bool) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !(multiline && r == '\n') {
			return ' '
		}
		return r
	}, s)
}

func getPage(ctx context.Context, client *http.Client, target string) (page, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return page{}, err
	}
	res, err := client.Do(r)
	if err != nil {
		return page{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return page{}, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	const limit = 8 << 20
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return page{}, err
	}
	var p page
	if len(data) > limit || json.Unmarshal(data, &p) != nil || p.Items == nil {
		return page{}, fmt.Errorf("invalid API page")
	}
	for i := range p.Items {
		m := &p.Items[i]
		if m.ID == "" {
			return page{}, fmt.Errorf("missing item ID")
		}
		m.DisplayName, m.Sender = clean(m.DisplayName, false), clean(m.Sender, false)
		m.Text, m.SentAt = clean(m.Text, true), clean(m.SentAt, false)
	}
	return p, nil
}

func load(ctx context.Context, client *http.Client, base string, r request, demo bool) (page, error) {
	if demo {
		return demoPage(r.chat), nil
	}
	p, err := getPage(ctx, client, r.url(base))
	if err != nil || r.head == "" {
		return p, err
	}
	// Bridge a burst larger than one page before replacing the cached head.
	for p.NextBefore != "" {
		for _, m := range p.Items {
			if m.ID == r.head || (r.date != "" && m.SentAt != "" && m.SentAt < r.date) {
				return p, nil
			}
		}
		r.before = p.NextBefore
		next, err := getPage(ctx, client, r.url(base))
		if err != nil {
			return page{}, err
		}
		count := len(p.Items)
		p.merge(next, true)
		if len(p.Items) == count {
			return page{}, fmt.Errorf("pagination did not advance")
		}
	}
	return p, nil
}

func demoPage(chat string) page {
	switch chat {
	case "":
		return page{Items: []item{{ID: "alex", DisplayName: "Alex Chen"}, {ID: "weekend", DisplayName: "Weekend plans"}, {ID: "sam", DisplayName: "Sam Rivera"}}}
	case "alex":
		return page{Items: []item{
			{ID: "3", Text: "Perfect. Meet at the coffee shop at 10? ☕", SentAt: "2026-09-05T14:12:00Z", Sender: "Alex"},
			{ID: "2", Text: "Yes! I can bring the camera. Let's take the trail along the lake if the weather holds.", SentAt: "2026-09-05T14:10:00Z", IsFromMe: true},
			{ID: "1", Text: "Hey, are you still up for a walk tomorrow?", SentAt: "2026-09-05T14:08:00Z", Sender: "Alex"},
		}}
	default:
		return page{Items: []item{}}
	}
}
