package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Both API lists use the same ID/cursor contract. Only presentation fields are needed.
type item struct {
	ID, DisplayName, Text, SentAt, Sender string
	IsFromMe                              bool
	Attachments                           []attachment
}

func equalItem(a, b item) bool {
	return a.ID == b.ID && a.DisplayName == b.DisplayName && a.Text == b.Text &&
		a.SentAt == b.SentAt && a.Sender == b.Sender && a.IsFromMe == b.IsFromMe &&
		slices.Equal(a.Attachments, b.Attachments)
}

type page struct {
	Items      []item
	NextBefore string
}

// merge retains loaded history below the refreshed head, or appends an older page.
// An unchanged poll keeps the existing slice and its prepared transcript layout.
func (p *page) merge(next page, older bool) bool {
	if !older && next.NextBefore != "" && len(next.Items) <= len(p.Items) && slices.EqualFunc(p.Items[:len(next.Items)], next.Items, equalItem) {
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
	changed := !slices.EqualFunc(p.Items, next.Items, equalItem)
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
	if r.chat != "" {
		q.Set("media", "metadata")
	} else {
		q.Set("sort", "latest")
	}
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

var errPageTooLarge = errors.New("API page exceeds 64 MiB")

func getPage(ctx context.Context, client *http.Client, target string) (page, error) {
	for {
		p, err := getPageOnce(ctx, client, target)
		if !errors.Is(err, errPageTooLarge) {
			return p, err
		}
		// Keep the cursor and full image data; ask for fewer messages instead.
		u, _ := url.Parse(target) // The HTTP request already validated this URL.
		q := u.Query()
		count, _ := strconv.Atoi(q.Get("limit"))
		if count <= 1 {
			return page{}, err
		}
		q.Set("limit", strconv.Itoa(count/2))
		u.RawQuery = q.Encode()
		target = u.String()
	}
}

func getPageOnce(ctx context.Context, client *http.Client, target string) (page, error) {
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
	const limit = 64 << 20 // Base64 media pages can exceed the former text-only 8 MiB limit.
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return page{}, err
	}
	if len(data) > limit {
		return page{}, errPageTooLarge
	}
	var p page
	if json.Unmarshal(data, &p) != nil || p.Items == nil {
		return page{}, fmt.Errorf("invalid API page")
	}
	for i := range p.Items {
		m := &p.Items[i]
		if m.ID == "" {
			return page{}, fmt.Errorf("missing item ID")
		}
		m.DisplayName, m.Sender = clean(m.DisplayName, false), clean(m.Sender, false)
		m.Text, m.SentAt = clean(m.Text, true), clean(m.SentAt, false)
		for j := range m.Attachments {
			a := &m.Attachments[j]
			a.Filename, a.MimeType = clean(a.Filename, false), clean(a.MimeType, false)
		}
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
	p := page{}
	switch chat {
	case "":
		return page{Items: []item{{ID: "alex", DisplayName: "Alex Chen"}, {ID: "weekend", DisplayName: "Weekend plans"}, {ID: "sam", DisplayName: "Sam Rivera"}}}
	case "alex":
		p.Items = []item{
			{ID: "3", Text: "Perfect. Meet at the coffee shop at 10? ☕", SentAt: "2026-09-05T14:12:00Z", Sender: "Alex"},
			{ID: "2", Text: "Yes! I can bring the camera. Let's take the trail along the lake if the weather holds.", SentAt: "2026-09-05T14:10:00Z", IsFromMe: true},
			{ID: "1", Text: "Hey, are you still up for a walk tomorrow?", SentAt: "2026-09-05T14:08:00Z", Sender: "Alex"},
		}
	}
	texts := []string{
		"Coffee before the walk? ☕",
		"I checked the route. The lakeside trail is open, but the north entrance is closed. Let's meet by the bridge and take the longer path back.",
		"Sounds good! I'll bring snacks and water.",
		"Packing list:\nCamera, spare battery, and a rain jacket.\nThe forecast says it might rain after lunch.",
		"See you there ❤️",
	}
	for i := 100; i > 0; i-- {
		p.Items = append(p.Items, item{
			ID:       fmt.Sprintf("%s-history-%d", chat, i),
			Text:     fmt.Sprintf("%03d: %s", i, texts[(i-1)%len(texts)]),
			SentAt:   fmt.Sprintf("2026-09-04T%02d:%02d:00Z", 9+i/60, i%60),
			Sender:   map[string]string{"alex": "Alex", "weekend": "Jamie", "sam": "Sam"}[chat],
			IsFromMe: i%3 == 0,
		})
	}
	return p
}
