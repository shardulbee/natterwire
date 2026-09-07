package main

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"unicode/utf8"
)

// A new session rejects retries after restart. Never evict attempts within a
// session: even an AppleScript error may have happened after Messages accepted it.
type sendAPI struct {
	d        *database
	session  string
	send     func(string, string) error
	mu       sync.Mutex
	attempts map[string]*sendAttempt
}

type sendAttempt struct {
	Chat, Text, Error string
	Status            int
}

func newSendAPI(d *database, sender func(string, string) error) *sendAPI {
	return &sendAPI{d: d, session: rand.Text(), send: sender, attempts: make(map[string]*sendAttempt)}
}

func (s *sendAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	isSend := r.Method == "POST" && len(parts) == 3 && parts[0] == "chats" && parts[2] == "messages"
	if !isSend && r.URL.Path != "/send-session" {
		s.d.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	reply := func(code int, message string) {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": message, "accepted": code == 200})
	}
	if r.Header.Get("Origin") != "" {
		reply(403, "browser send requests are not allowed")
		return
	}
	if !isSend {
		if r.Method != "GET" {
			reply(405, "method not allowed")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"session": s.session})
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		reply(415, "application/json required")
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	data, readErr := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if readErr != nil || !utf8.Valid(data) || dec.Decode(&body) != nil || dec.Decode(new(any)) != io.EOF || strings.TrimSpace(body.Text) == "" || len(body.Text) > 16000 || strings.ContainsRune(body.Text, 0) {
		reply(400, "text must contain 1–16000 UTF-8 bytes, without NUL")
		return
	}
	key := r.Header.Get("Idempotency-Key")
	if !strings.HasPrefix(key, s.session+":") || len(key) <= len(s.session)+1 || len(key) > 160 {
		reply(409, "invalid or expired send session; check Messages before starting another send")
		return
	}
	guid, err := unopaque(parts[1])
	if err != nil || guid == "" {
		reply(400, "invalid chat identifier")
		return
	}
	s.mu.Lock()
	if old := s.attempts[key]; old != nil {
		status, message := old.Status, old.Error
		if old.Chat != guid || old.Text != body.Text {
			status, message = 409, "request ID already used for different content"
		}
		s.mu.Unlock()
		reply(status, message)
		return
	}
	if len(s.attempts) >= 10000 {
		s.mu.Unlock()
		reply(503, "send session full; restart API only after checking outstanding sends")
		return
	}
	var count int
	err = s.d.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM chat WHERE guid=?", guid).Scan(&count)
	if err != nil || count != 1 {
		s.mu.Unlock()
		reply(400, "conversation must exist uniquely in Messages database")
		return
	}
	a := &sendAttempt{Chat: guid, Text: body.Text, Status: 409, Error: "send still pending; retry only with the same request ID"}
	s.attempts[key] = a
	s.mu.Unlock()
	err = s.send(guid, body.Text)
	s.mu.Lock()
	a.Status, a.Error = 200, ""
	if err != nil {
		a.Status, a.Error = 502, err.Error()
	}
	status, message := a.Status, a.Error
	s.mu.Unlock()
	reply(status, message)
}
