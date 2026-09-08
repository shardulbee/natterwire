package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"
	"golang.org/x/crypto/hkdf"
)

type pushHTTPFunc func(*http.Request) (*http.Response, error)

func (f pushHTTPFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

func fixtureWriter(t *testing.T, d *database) *sql.DB {
	t.Helper()
	var seq int
	var name, path string
	if err := d.db.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	w, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return w
}

func TestUnreadSource(t *testing.T) {
	d := fixture(t, nil)
	messages, latest, err := d.incoming(context.Background(), 0)
	if err != nil || latest != 16 || len(messages) != 3 || messages[0].Row != 3 || messages[1].Row != 10 || messages[2].Row != 14 {
		t.Fatal("incoming filter included read, outgoing, hidden or system rows", messages, latest, err)
	}
	w := fixtureWriter(t, d)
	for _, check := range []struct {
		sql   string
		count int64
	}{
		{"PRAGMA journal_mode=WAL", 2},
		{"UPDATE message SET is_read=1 WHERE ROWID=3", 1},
		{"UPDATE message SET is_read=1 WHERE ROWID=14", 0},
	} {
		if _, err := w.Exec(check.sql); err != nil {
			t.Fatal(err)
		}
		page := get[Chat](t, d, "/chats?sort=latest")
		if got := page.Items[0].UnreadCount; got == nil || *got != check.count {
			t.Fatalf("unread = %v; want %d", got, check.count)
		}
	}
	if _, err := d.db.Exec("UPDATE message SET is_read=1"); err == nil {
		t.Fatal("Messages connection allowed a write")
	}
}

func testSubscription(t *testing.T) (webpush.Subscription, *ecdh.PrivateKey) {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return webpush.Subscription{Endpoint: "https://fcm.googleapis.com/send/fixture", Keys: webpush.Keys{
		P256dh: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		Auth:   base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 16)),
	}}, key
}

func pushRequest(p *pushAPI, method string, sub any) *httptest.ResponseRecorder {
	body, _ := json.Marshal(sub)
	r := httptest.NewRequest(method, "/push", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	return w
}

// Decrypt as the receiving browser to verify RFC 8291 framing and privacy, not
// merely that an HTTP request was made. No vendor service is contacted in tests.
func decryptPush(t *testing.T, body []byte, sub webpush.Subscription, key *ecdh.PrivateKey) []byte {
	t.Helper()
	serverKey, err := ecdh.P256().NewPublicKey(body[21 : 21+int(body[20])])
	if err != nil {
		t.Fatal(err)
	}
	shared, err := key.ECDH(serverKey)
	if err != nil {
		t.Fatal(err)
	}
	derive := func(secret, salt, info []byte, size int) []byte {
		result := make([]byte, size)
		if _, err := io.ReadFull(hkdf.New(sha256.New, secret, salt, info), result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	auth, _ := base64.RawURLEncoding.DecodeString(sub.Keys.Auth)
	info := append([]byte("WebPush: info\x00"), key.PublicKey().Bytes()...)
	info = append(info, serverKey.Bytes()...)
	ikm := derive(shared, auth, info, 32)
	block, err := aes.NewCipher(derive(ikm, body[:16], []byte("Content-Encoding: aes128gcm\x00"), 16))
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := gcm.Open(nil, derive(ikm, body[:16], []byte("Content-Encoding: nonce\x00"), 12), body[21+int(body[20]):], nil)
	if err != nil {
		t.Fatal(err)
	}
	plain = bytes.TrimRight(plain, "\x00")
	if len(plain) == 0 || plain[len(plain)-1] != 2 {
		t.Fatal("invalid final record delimiter")
	}
	return plain[:len(plain)-1]
}

func TestPushPersistenceDeliveryAndExpiry(t *testing.T) {
	d := fixture(t, nil)
	p, err := newPushAPI(d, d, filepath.Join(t.TempDir(), "push.json"))
	if err != nil {
		t.Fatal(err)
	}
	sub, key := testSubscription(t)
	if w := pushRequest(p, "POST", sub); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var calls int
	status := 201
	p.client = pushHTTPFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Content-Encoding") != "aes128gcm" || !strings.HasPrefix(r.Header.Get("Authorization"), "vapid ") || r.Header.Get("TTL") != "300" {
			t.Fatal("missing encrypted push headers")
		}
		var payload map[string]string
		if err := json.Unmarshal(decryptPush(t, body, sub, key), &payload); err != nil {
			t.Fatal(err)
		}
		if len(payload) != 1 || payload["chat"] != opaque("iMessage;-;alex@example.invalid") {
			t.Fatalf("unexpected notification payload: %v", payload)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	if err := p.poll(context.Background()); err != nil || calls != 0 {
		t.Fatal("opt-in replayed historical messages", err)
	}
	w := fixtureWriter(t, d)
	if _, err := w.Exec(`INSERT INTO message(ROWID,guid,text,date,is_from_me,is_read) VALUES
	 (17,'new','Do not include this in push',900000000000000001,0,0),
	 (18,'sent','Outgoing',900000000000000002,1,0),
	 (19,'read','Already read',900000000000000003,0,1);
	 INSERT INTO chat_message_join VALUES(1,17),(1,18),(1,19)`); err != nil {
		t.Fatal(err)
	}
	status = 503
	if err := p.poll(context.Background()); err == nil || p.state.Subscriptions[sub.Endpoint].Cursor != 16 {
		t.Fatal("failed delivery advanced cursor")
	}
	status = 201
	if err := p.poll(context.Background()); err != nil || calls != 2 || p.state.Subscriptions[sub.Endpoint].Cursor != 19 {
		t.Fatal("retry did not deliver incoming unread message", err, calls)
	}
	restarted, err := newPushAPI(d, d, p.path)
	if err != nil || restarted.state.PublicKey != p.state.PublicKey || restarted.state.PrivateKey != p.state.PrivateKey {
		t.Fatal("VAPID state not retained", err)
	}
	restarted.client = p.client
	if err := restarted.poll(context.Background()); err != nil || calls != 2 {
		t.Fatal("restart replayed delivered messages", err)
	}
	info, err := os.Stat(p.path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("push state must be owner-only", err)
	}
	get := pushRequest(p, "GET", nil)
	if strings.Contains(get.Body.String(), p.state.PrivateKey) || strings.Contains(get.Body.String(), sub.Endpoint) {
		t.Fatal("private push state exposed")
	}
	if _, err := w.Exec("UPDATE message SET is_read=0 WHERE ROWID=19; INSERT INTO message(ROWID,text,date,is_from_me,is_read) VALUES(20,'Next',900000000000000004,0,0); INSERT INTO chat_message_join VALUES(1,20)"); err != nil {
		t.Fatal(err)
	}
	status = 410
	if err := restarted.poll(context.Background()); err != nil || len(restarted.state.Subscriptions) != 0 {
		t.Fatal("expired endpoint retained", err)
	}
	restarted, err = newPushAPI(d, d, p.path)
	if err != nil || len(restarted.state.Subscriptions) != 0 {
		t.Fatal("expiry not persisted", err)
	}
}

func TestPushOptOutDuringDelivery(t *testing.T) {
	d := fixture(t, nil)
	p, err := newPushAPI(d, d, filepath.Join(t.TempDir(), "push.json"))
	if err != nil {
		t.Fatal(err)
	}
	sub, _ := testSubscription(t)
	if pushRequest(p, "POST", sub).Code != 200 {
		t.Fatal("could not subscribe")
	}
	w := fixtureWriter(t, d)
	if _, err := w.Exec(`INSERT INTO message(ROWID,text,date,is_from_me,is_read) VALUES(17,'one',900000000000000001,0,0),(18,'two',900000000000000002,0,0); INSERT INTO chat_message_join VALUES(1,17),(1,18)`); err != nil {
		t.Fatal(err)
	}
	calls := 0
	p.client = pushHTTPFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if pushRequest(p, "DELETE", sub).Code != 200 {
			t.Fatal("opt-out blocked by delivery")
		}
		return &http.Response{StatusCode: 201, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	if err := p.poll(context.Background()); err != nil || calls != 1 || len(p.state.Subscriptions) != 0 {
		t.Fatal("opt-out did not stop remaining deliveries", err, calls)
	}
}

func TestPushValidationAndConcurrentSettings(t *testing.T) {
	d := fixture(t, nil)
	p, err := newPushAPI(d, d, filepath.Join(t.TempDir(), "push.json"))
	if err != nil {
		t.Fatal(err)
	}
	sub, _ := testSubscription(t)
	for _, endpoint := range []string{"http://fcm.googleapis.com/x", "https://localhost/x", "https://127.0.0.1/x", "https://fcm.googleapis.com.evil.invalid/x", "https://evil.invalid/x", "https://fcm.googleapis.com:443/x", "https://user@fcm.googleapis.com/x"} {
		bad := sub
		bad.Endpoint = endpoint
		if w := pushRequest(p, "POST", bad); w.Code != 400 {
			t.Fatal("accepted invalid endpoint", endpoint, w.Code)
		}
	}
	for _, endpoint := range []string{"https://web.push.apple.com/x", "https://updates.push.services.mozilla.com/x", "https://wns2.notify.windows.com/x"} {
		if !validPushEndpoint(endpoint) {
			t.Fatal("rejected supported vendor", endpoint)
		}
	}
	bad := sub
	bad.Keys.Auth = "invalid"
	if pushRequest(p, "POST", bad).Code != 400 {
		t.Fatal("accepted invalid encryption key")
	}
	r := httptest.NewRequest("POST", "/push", strings.NewReader("{}"))
	r.Header.Set("Origin", "https://evil.invalid")
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("accepted cross-origin subscription")
	}
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			for _, method := range []string{"POST", "GET", "DELETE"} {
				if w := pushRequest(p, method, sub); w.Code != 200 {
					t.Error("settings request failed", w.Code)
				}
			}
		})
	}
	wg.Go(func() {
		for range 10 {
			if err := p.poll(context.Background()); err != nil {
				t.Error(err)
			}
		}
	})
	wg.Wait()
}
