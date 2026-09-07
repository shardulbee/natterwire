//go:build !darwin

package main

import (
	"strings"
	"testing"
)

func TestLinuxSendUnsupported(t *testing.T) {
	s := newSendAPI(fixture(t, nil), "secret", sendText)
	w := sendCall(s, "iMessage;+;weekend", s.session+":1", `{"text":"hello"}`, "secret", "")
	if w.Code != 502 || !strings.Contains(w.Body.String(), "unsupported") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
