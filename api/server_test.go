package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image/png"
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
	}{{"/", `id="app"`, 200}, {"/shell.js", "loadChats()", 200}, {"/chats", "", http.StatusTeapot}, {"/missing", "", 404}} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", tc.path, nil))
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.contains) {
			t.Errorf("GET %s: %d %q", tc.path, w.Code, w.Body.String())
		}
	}
}

func TestInstallableManifest(t *testing.T) {
	handler := webHandler(http.NotFoundHandler())
	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", path, w.Code)
		}
		return w
	}
	if !strings.Contains(get("/").Body.String(), `rel="manifest" href="manifest.webmanifest"`) {
		t.Fatal("missing manifest link")
	}
	w := get("/manifest.webmanifest")
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/manifest+json") {
		t.Fatalf("manifest content type: %s", w.Header().Get("Content-Type"))
	}
	var manifest struct {
		ID, Name, Display, Scope string
		StartURL                 string `json:"start_url"`
		Icons                    []struct{ Src, Sizes, Type string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.ID != "/" || manifest.Name != "Natterwire" || manifest.Display != "standalone" || manifest.Scope != "/" || manifest.StartURL != "/" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	get(manifest.StartURL)
	sizes := map[string]bool{}
	for _, icon := range manifest.Icons {
		w := get("/" + icon.Src)
		config, err := png.DecodeConfig(w.Body)
		if err != nil {
			t.Fatal(err)
		}
		if icon.Type != "image/png" || w.Header().Get("Content-Type") != icon.Type || icon.Sizes != fmt.Sprintf("%dx%d", config.Width, config.Height) {
			t.Fatalf("icon metadata does not match PNG: %+v", icon)
		}
		sizes[icon.Sizes] = true
	}
	if !sizes["192x192"] || !sizes["512x512"] {
		t.Fatal("missing install icon sizes")
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
