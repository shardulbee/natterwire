package main

import (
	"context"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

type pushSubscription struct {
	webpush.Subscription
	Cursor int64 `json:"cursor"`
}

type pushState struct {
	PrivateKey    string                      `json:"privateKey"`
	PublicKey     string                      `json:"publicKey"`
	Subscriptions map[string]pushSubscription `json:"subscriptions"`
}

type pushAPI struct {
	next   http.Handler
	d      *database
	path   string
	mu     sync.Mutex // protects state and its atomic file replacement, never held during network I/O
	state  pushState
	client webpush.HTTPClient
}

func newPushAPI(d *database, next http.Handler, path string) (*pushAPI, error) {
	p := &pushAPI{d: d, next: next, path: expandHome(path), client: pushClient()}
	data, err := os.ReadFile(p.path)
	if err == nil {
		err = json.Unmarshal(data, &p.state)
		if err == nil && (p.state.PrivateKey == "" || p.state.PublicKey == "" || p.state.Subscriptions == nil) {
			err = errors.New("incomplete push state")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		p.state.PrivateKey, p.state.PublicKey, err = webpush.GenerateVAPIDKeys()
		p.state.Subscriptions = make(map[string]pushSubscription)
		if err == nil {
			err = p.save()
		}
	}
	return p, err
}

// The file contains device capabilities and a private signing key, not message text.
// Messages' SQLite connection remains query_only and is never used for push state.
func (p *pushAPI) save() error {
	if err := os.MkdirAll(filepath.Dir(p.path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(p.state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p.path), ".push-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), p.path)
}

func validPushEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil || len(endpoint) > 4096 || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" {
		return false
	}
	host := u.Hostname()
	return host == "fcm.googleapis.com" || host == "updates.push.services.mozilla.com" ||
		strings.HasSuffix(host, ".push.apple.com") || strings.HasSuffix(host, ".notify.windows.com")
}

func pushClient() *http.Client {
	return &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{ // No environment proxy, redirects, or private-network dialing.
			ForceAttemptHTTP2: true,
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(address)
				if err != nil || !validPushEndpoint("https://"+host+"/") || port != "443" {
					return nil, errors.New("unsupported push host")
				}
				ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
				if err != nil {
					return nil, err
				}
				for _, ip := range ips {
					if !ip.IP.IsGlobalUnicast() || ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() {
						continue
					}
					conn, dialErr := (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
					if dialErr == nil {
						return conn, nil
					}
				}
				return nil, errors.New("push service unreachable")
			},
		},
	}
}

func (p *pushAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/push" {
		p.next.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	fail := func(code int, message string) {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
	}
	if !sameOrigin(r) {
		fail(403, "cross-origin push requests are not allowed")
		return
	}
	if r.Method == "GET" {
		_ = json.NewEncoder(w).Encode(map[string]string{"publicKey": p.state.PublicKey})
		return
	}
	if r.Method != "POST" && r.Method != "DELETE" {
		fail(405, "method not allowed")
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		fail(415, "application/json required")
		return
	}
	var sub webpush.Subscription
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	if dec.Decode(&sub) != nil || dec.Decode(new(any)) != io.EOF || !validPushEndpoint(sub.Endpoint) {
		fail(400, "invalid or unsupported push subscription")
		return
	}
	if r.Method == "POST" {
		key, keyErr := base64.RawURLEncoding.DecodeString(sub.Keys.P256dh)
		auth, authErr := base64.RawURLEncoding.DecodeString(sub.Keys.Auth)
		_, curveErr := ecdh.P256().NewPublicKey(key)
		if keyErr != nil || authErr != nil || len(auth) != 16 || curveErr != nil {
			fail(400, "invalid subscription encryption keys")
			return
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	old, exists := p.state.Subscriptions[sub.Endpoint]
	if r.Method == "DELETE" {
		delete(p.state.Subscriptions, sub.Endpoint)
	} else {
		if !exists && len(p.state.Subscriptions) >= 16 {
			fail(409, "device limit reached; disable notifications on an existing device")
			return
		}
		cursor := old.Cursor
		if !exists {
			// Opt-in never replays the user's historical inbox.
			if err := p.d.db.QueryRowContext(r.Context(), "SELECT COALESCE(MAX(ROWID),0) FROM message").Scan(&cursor); err != nil {
				fail(503, "Messages unavailable")
				return
			}
		}
		p.state.Subscriptions[sub.Endpoint] = pushSubscription{Subscription: sub, Cursor: cursor}
	}
	if err := p.save(); err != nil {
		if exists {
			p.state.Subscriptions[sub.Endpoint] = old
		} else {
			delete(p.state.Subscriptions, sub.Endpoint)
		}
		fail(500, "could not persist notification settings")
		return
	}
	_, _ = w.Write([]byte(`{"ok":true}`))
}

type pushMessage struct {
	Row  int64
	Chat string
}

func (d *database) incoming(ctx context.Context, after int64) ([]pushMessage, int64, error) {
	var latest int64
	if err := d.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(ROWID),0) FROM message").Scan(&latest); err != nil {
		return nil, after, err
	}
	filter := d.messageFilter() + " AND m.is_from_me=0"
	if d.message["is_read"] {
		filter += " AND m.is_read=0"
	}
	for _, col := range []string{"is_archived", "is_deleted"} {
		if d.chat[col] {
			filter += " AND COALESCE(c." + col + ",0)=0"
		}
	}
	rows, err := d.db.QueryContext(ctx, `SELECT m.ROWID, MIN(c.guid) FROM message m
	 JOIN chat_message_join cmj ON cmj.message_id=m.ROWID JOIN chat c ON c.ROWID=cmj.chat_id
	 WHERE m.ROWID>? AND m.ROWID<=? AND c.guid IS NOT NULL AND `+filter+` GROUP BY m.ROWID ORDER BY m.ROWID LIMIT 100`, after, latest)
	if err != nil {
		return nil, after, err
	}
	defer rows.Close()
	var messages []pushMessage
	for rows.Next() {
		var m pushMessage
		if err := rows.Scan(&m.Row, &m.Chat); err != nil {
			return nil, after, err
		}
		messages = append(messages, m)
	}
	if len(messages) == 100 {
		latest = messages[len(messages)-1].Row
	}
	return messages, latest, rows.Err()
}

func (p *pushAPI) poll(ctx context.Context) error {
	p.mu.Lock()
	subs := make([]pushSubscription, 0, len(p.state.Subscriptions))
	for _, sub := range p.state.Subscriptions {
		subs = append(subs, sub)
	}
	p.mu.Unlock()
	var failures []error
	for _, sub := range subs {
		messages, cursor, err := p.d.incoming(ctx, sub.Cursor)
		if err != nil {
			return err
		}
		expired := false
		for _, m := range messages {
			p.mu.Lock()
			current, subscribed := p.state.Subscriptions[sub.Endpoint]
			p.mu.Unlock()
			if !subscribed || current != sub {
				break
			}
			// Only an opaque chat ID travels in the encrypted payload. No names or text.
			payload, _ := json.Marshal(map[string]string{"chat": opaque(m.Chat)})
			var response *http.Response
			response, err = webpush.SendNotificationWithContext(ctx, payload, &sub.Subscription, &webpush.Options{
				HTTPClient: p.client, Subscriber: "https://github.com/shardulbee/natterwire",
				VAPIDPrivateKey: p.state.PrivateKey, VAPIDPublicKey: p.state.PublicKey,
				TTL: 300, Urgency: webpush.UrgencyNormal,
			})
			if err == nil {
				response.Body.Close()
				expired = response.StatusCode == 404 || response.StatusCode == 410
				if !expired && (response.StatusCode < 200 || response.StatusCode >= 300) {
					err = fmt.Errorf("push service status %d", response.StatusCode)
				}
			}
			if err != nil || expired {
				break
			}
		}
		if err != nil {
			// Retry next tick. Delivery before a crash or partial batch may be duplicated.
			failures = append(failures, errors.New("push delivery failed"))
			continue
		}
		p.mu.Lock()
		if current, ok := p.state.Subscriptions[sub.Endpoint]; ok && current == sub && (expired || cursor != sub.Cursor) {
			if expired {
				delete(p.state.Subscriptions, sub.Endpoint)
			} else {
				current.Cursor = cursor
				p.state.Subscriptions[sub.Endpoint] = current
			}
			if err := p.save(); err != nil {
				p.state.Subscriptions[sub.Endpoint] = sub
				failures = append(failures, errors.New("push cursor persistence failed"))
			}
		}
		p.mu.Unlock()
	}
	return errors.Join(failures...)
}

func (p *pushAPI) run(ctx context.Context) {
	timer := time.NewTicker(30 * time.Second)
	defer timer.Stop()
	for {
		if err := p.poll(ctx); err != nil && ctx.Err() == nil {
			log.Print("notification poll failed; will retry") // Never log endpoint capabilities or message data.
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}
