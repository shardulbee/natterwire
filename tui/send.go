package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type sendRequest struct{ chat, text, key string }
type sendResult struct {
	request sendRequest
	err     error
}

func postText(ctx context.Context, client *http.Client, base string, attempt sendRequest) sendResult {
	result := sendResult{request: attempt}
	do := func(method, path string, body []byte, value any) error {
		r, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, bytes.NewReader(body))
		if err != nil {
			return err
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", result.request.key)
		res, err := client.Do(r)
		if err != nil {
			return fmt.Errorf("outcome unknown; check Messages, Enter retries the same request")
		}
		defer res.Body.Close()
		data, err := io.ReadAll(io.LimitReader(res.Body, 4096))
		if err != nil {
			return fmt.Errorf("outcome unknown; response unreadable")
		}
		if res.StatusCode != 200 {
			var failure struct{ Error string }
			_ = json.Unmarshal(data, &failure)
			return fmt.Errorf("HTTP %d: %s", res.StatusCode, clean(failure.Error, false))
		}
		return json.Unmarshal(data, value)
	}
	if attempt.key == "" {
		var session struct{ Session string }
		if result.err = do("GET", "/send-session", nil, &session); result.err != nil {
			return result
		}
		if session.Session == "" {
			result.err = fmt.Errorf("missing send session")
			return result
		}
		result.request.key = session.Session + ":" + rand.Text()
	}
	body, _ := json.Marshal(map[string]string{"text": attempt.text})
	var ack struct{ Accepted bool }
	result.err = do("POST", "/chats/"+attempt.chat+"/messages", body, &ack)
	if result.err == nil && !ack.Accepted {
		result.err = fmt.Errorf("outcome unknown; missing acceptance receipt")
	}
	return result
}

func (a *app) queueSend() {
	c := a.current()
	if c == nil || c.sending {
		return
	}
	defer func() { c.sendStatus = a.status }()
	text := c.draft.String()
	if a.demo {
		a.status = "Not sent: demo cannot send. Draft unchanged."
		return
	}
	if strings.TrimSpace(text) == "" || len(text) > 16000 {
		a.status = "Not sent: enter 1–16000 bytes of text."
		return
	}
	if c.attempt != nil && c.attempt.text != text {
		a.status = "Not sent: unresolved send. Restore original draft to retry; check Messages before restarting TUI."
		return
	}
	if c.attempt == nil {
		c.attempt = &sendRequest{chat: a.opened, text: text}
	}
	c.sending = true
	a.status = "Sending…"
	a.pendingSend = c.attempt
}

func (a *app) acceptSend(r sendResult) {
	c := a.cache[r.request.chat]
	c.sending = false
	c.attempt = &r.request
	status := "Not sent or unconfirmed: "
	if r.err != nil {
		status += r.err.Error() + ". Draft retained."
		if r.request.key == "" {
			c.attempt = nil
		} // Session fetch failed; no POST was attempted.
	} else {
		if c.draft.String() == r.request.text {
			c.draft.SetContent("")
		}
		c.attempt = nil
		status = "Accepted by Messages. Delivery is not confirmed."
		a.refresh()
	}
	c.sendStatus = status
	if a.opened == r.request.chat {
		a.status = status
	}
}
