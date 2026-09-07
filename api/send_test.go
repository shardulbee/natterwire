package main

import (
	"errors"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func sendCall(s *sendAPI, id, key, body, token, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/chats/"+opaque(id)+"/messages", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Origin", origin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestSendValidationAndRouting(t *testing.T) {
	d := fixture(t, nil)
	var calls int
	s := newSendAPI(d, "secret", func(guid, text string) error {
		calls++
		if guid != "iMessage;+;weekend" || text != "hello\n\"世界\"" {
			t.Fatalf("misrouted: %q %q", guid, text)
		}
		return nil
	})
	body := `{"text":"hello\n\"世界\""}`
	for _, tc := range []struct {
		id, key, body, token, origin string
		status                       int
	}{
		{"iMessage;+;weekend", s.session + ":1", body, "", "", 403},
		{"iMessage;+;weekend", s.session + ":1", body, "secret", "https://evil.invalid", 403},
		{"iMessage;+;weekend", "old:1", body, "secret", "", 409},
		{"missing", s.session + ":1", body, "secret", "", 400},
		{"iMessage;+;weekend", s.session + ":1", `{"text":" "}`, "secret", "", 400},
		{"iMessage;+;weekend", s.session + ":1", `{"text":"x","attachment":"x"}`, "secret", "", 400},
		{"iMessage;+;weekend", s.session + ":1", `{"text":"x"}{}`, "secret", "", 400},
		{"iMessage;+;weekend", s.session + ":1", `{"text":"\u0000"}`, "secret", "", 400},
		{"iMessage;+;weekend", s.session + ":1", `{"text":"` + strings.Repeat("a", 16001) + `"}`, "secret", "", 400},
		{"iMessage;+;weekend", s.session + ":1", body, "secret", "", 200},
		{"iMessage;+;weekend", s.session + ":1", body, "secret", "", 200},
		{"iMessage;-;alex@example.invalid", s.session + ":1", body, "secret", "", 409},
		{"iMessage;+;weekend", s.session + ":1", `{"text":"different"}`, "secret", "", 409},
	} {
		w := sendCall(s, tc.id, tc.key, tc.body, tc.token, tc.origin)
		if w.Code != tc.status {
			t.Fatalf("got %d want %d: %s", w.Code, tc.status, w.Body.String())
		}
	}
	if calls != 1 {
		t.Fatalf("sent %d times", calls)
	}
	if _, err := d.db.Exec("UPDATE chat SET guid='changed'"); err == nil {
		t.Fatal("database became writable")
	}
	restarted := newSendAPI(d, "secret", s.send)
	if w := sendCall(restarted, "iMessage;+;weekend", s.session+":1", body, "secret", ""); w.Code != 409 {
		t.Fatal("restart accepted stale retry")
	}
	s.token = ""
	if w := sendCall(s, "iMessage;+;weekend", s.session+":2", body, "secret", ""); w.Code != 403 {
		t.Fatal("missing token enabled sending")
	}
}

func TestSendPendingAndAmbiguousFailure(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s := newSendAPI(fixture(t, nil), "secret", func(string, string) error {
		calls.Add(1)
		close(started)
		<-finish
		return errors.New("outcome unknown")
	})
	call := func() *httptest.ResponseRecorder {
		return sendCall(s, "iMessage;+;weekend", s.session+":1", `{"text":"hello"}`, "secret", "")
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- call() }()
	<-started
	if w := call(); w.Code != 409 {
		t.Fatalf("pending = %d", w.Code)
	}
	close(finish)
	if w := <-done; w.Code != 502 {
		t.Fatalf("failure = %d", w.Code)
	}
	if w := call(); w.Code != 502 || calls.Load() != 1 {
		t.Fatal("ambiguous failure retried delivery")
	}
}
