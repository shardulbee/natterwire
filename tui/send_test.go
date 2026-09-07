package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.rockorager.dev/vaxis"
)

func TestSendFlow(t *testing.T) {
	for _, success := range []bool{false, true} {
		t.Run(fmt.Sprint(success), func(t *testing.T) {
			var keys []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("missing write auth")
				}
				if r.URL.Path == "/send-session" {
					fmt.Fprint(w, `{"session":"test"}`)
					return
				}
				if r.Method != "POST" || r.URL.Path != "/chats/group/messages" {
					t.Errorf("wrong route %s %s", r.Method, r.URL.Path)
				}
				var body struct{ Text string }
				if json.NewDecoder(r.Body).Decode(&body) != nil || body.Text != "hello 👋" {
					t.Error("wrong text")
				}
				keys = append(keys, r.Header.Get("Idempotency-Key"))
				if success {
					fmt.Fprint(w, `{"accepted":true}`)
				} else {
					w.WriteHeader(502)
					fmt.Fprint(w, `{"error":"outcome unknown"}`)
				}
			}))
			defer server.Close()
			a := newApp(server.URL, false)
			a.token = "secret"
			a.chats.Items = []item{{ID: "group"}, {ID: "other"}}
			a.activate(insert)
			c := a.current()
			c.draft.SetContent("hello 👋")
			a.key(vaxis.Key{Keycode: vaxis.KeyEnter})
			original := a.pendingSend
			a.key(vaxis.Key{Keycode: vaxis.KeyEnter})
			if a.pendingSend != original || !c.sending {
				t.Fatal("duplicate queued")
			}
			a.key(vaxis.Key{Keycode: 'x', Text: "x"})
			if c.draft.String() != "hello 👋" {
				t.Fatal("edited pending draft")
			}
			a.busy = true // Isolate send worker from the read worker.
			a.pump(context.Background(), server.Client())
			a.selected = 1
			a.activate(insert)
			a.current().draft.SetContent("other draft")
			var result sendResult
			select {
			case result = <-a.sendResults:
			case <-time.After(time.Second):
				t.Fatal("send worker blocked")
			}
			a.acceptSend(result)
			if a.current().draft.String() != "other draft" {
				t.Fatal("cleared different conversation")
			}
			if success {
				if c.draft.String() != "" || c.attempt != nil || !a.pendingChats {
					t.Fatal("success did not clear/refresh")
				}
			} else {
				if c.draft.String() != "hello 👋" || c.attempt.key == "" {
					t.Fatal("failure lost draft or retry key")
				}
				a.selected = 0
				a.activate(insert)
				if !strings.Contains(a.status, "outcome unknown") {
					t.Fatal("lost background send error")
				}
				c.draft.SetContent("edited")
				a.queueSend()
				if a.pendingSend != nil || !strings.Contains(a.status, "unresolved") {
					t.Fatal("new send allowed after ambiguity")
				}
				c.draft.SetContent("hello 👋")
				a.queueSend()
				retry := postText(context.Background(), server.Client(), server.URL, a.token, *a.pendingSend)
				a.acceptSend(retry)
				if len(keys) != 2 || keys[0] == "" || keys[0] != keys[1] {
					t.Fatal("retry changed identity")
				}
			}
		})
	}
}

func TestSendMissingReceipt(t *testing.T) {
	for _, body := range []string{"null", "{}", "", `{"accepted":false}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		r := postText(context.Background(), s.Client(), s.URL, "secret", sendRequest{chat: "group", text: "hello", key: "session:key"})
		s.Close()
		if r.err == nil || r.request.key != "session:key" {
			t.Fatal("ambiguous reply treated as success")
		}
	}
}

func TestSendLostResponse(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	}))
	defer s.Close()
	r := postText(context.Background(), s.Client(), s.URL, "secret", sendRequest{chat: "group", text: "hello", key: "session:key"})
	if r.err == nil || r.request.key != "session:key" || calls.Load() != 1 {
		t.Fatal("lost response discarded identity or triggered automatic new send")
	}
}
