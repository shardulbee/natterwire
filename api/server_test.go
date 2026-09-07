package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebHandler(t *testing.T) {
	api := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	handler := webHandler(api)
	for _, tc := range []struct {
		path, contains string
		status         int
	}{{"/", `id="app"`, 200}, {"/shell.js", "loadChats()", 200}, {"/chats", "", http.StatusTeapot}, {"/natterwire/", `id="app"`, 200}, {"/natterwire/shell.js", "loadChats()", 200}, {"/natterwire/chats", "", http.StatusTeapot}, {"/missing", "", 404}} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.contains) {
			t.Errorf("GET %s: %d %q", tc.path, w.Code, w.Body.String())
		}
	}
}

func TestServeQuitAndPortConflict(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	if err := serve(context.Background(), occupied.Addr().String(), http.NotFoundHandler(), func() { t.Error("announced ready before bind") }); err == nil {
		t.Fatal("port conflict ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() { done <- serve(ctx, "127.0.0.1:0", http.NotFoundHandler(), func() { close(ready) }) }()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("not ready")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("quit did not stop server")
	}
}
