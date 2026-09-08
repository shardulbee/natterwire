package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParseLinkPreviewPrecedenceAndRelativeImage(t *testing.T) {
	base, _ := url.Parse("https://example.com/articles/page")
	html := `<html><head>
		<title>HTML &amp; title</title>
		<meta name="twitter:title" content="Twitter title">
		<meta property="og:title" content="OG &amp; title">
		<meta name="twitter:description" content="Twitter description">
		<meta property="og:description" content="OG description">
		<meta name="twitter:image" content="/fallback.jpg">
		<meta property="og:image" content="../image?a=1&amp;b=2">
	</head></html>`
	got := parseLinkPreview(strings.NewReader(html), base)
	want := linkPreview{"OG & title", "OG description", "https://example.com/image?a=1&b=2"}
	if got != want {
		t.Fatalf("got %#v, want %#v", got, want)
	}

	fallback := parseLinkPreview(strings.NewReader(`<title>HTML &amp; title</title><meta name="twitter:title" content="Twitter">`), base)
	if fallback.Title != "Twitter" {
		t.Fatalf("twitter fallback lost: %#v", fallback)
	}
}

func TestLinkPreviewURLAndAddressProtection(t *testing.T) {
	for _, raw := range []string{"", "/relative", "file:///etc/passwd", "http://user:pass@example.com", "https://example.com@127.0.0.1"} {
		if _, err := parsePreviewURL(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	for _, raw := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "100.64.0.1", "192.0.2.1", "::1", "fc00::1", "fe80::1", "2001:db8::1", "64:ff9b::7f00:1", "2002:7f00:1::", "::ffff:127.0.0.1"} {
		if publicIP(net.ParseIP(raw)) {
			t.Errorf("address considered public: %s", raw)
		}
	}
	if !publicIP(net.ParseIP("8.8.8.8")) || !publicIP(net.ParseIP("2606:4700:4700::1111")) {
		t.Fatal("public address rejected")
	}
}

func TestLinkPreviewHandlerValidationOriginAndCache(t *testing.T) {
	a := newLinkPreviewAPI(http.NotFoundHandler())
	request := func(target, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, target, nil)
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		return w
	}
	if got := request("/link-preview?url=file%3A%2F%2F%2Fetc%2Fpasswd", ""); got.Code != 400 {
		t.Fatalf("unsafe URL status %d", got.Code)
	}
	if got := request("/link-preview?url=https%3A%2F%2Fexample.com", "https://evil.example"); got.Code != 403 {
		t.Fatalf("cross-origin status %d", got.Code)
	}

	a.store("https://example.com", linkPreview{Title: "cached"}, true)
	got := request("/link-preview?url=https%3A%2F%2Fexample.com", "http://example.com")
	if got.Code != 200 || !strings.Contains(got.Body.String(), `"title":"cached"`) {
		t.Fatalf("cache response: %d %s", got.Code, got.Body.String())
	}
	for i := 0; i < 300; i++ {
		a.store(string(rune(i+1)), linkPreview{}, false)
	}
	if len(a.cache) != 256 {
		t.Fatalf("cache grew to %d", len(a.cache))
	}
	for i := 0; i < 300; i++ {
		key := "https://expired.example"
		a.store(key, linkPreview{}, false)
		a.cache[key] = previewCacheEntry{expires: time.Now().Add(-time.Hour)}
		if _, ok := a.cached(key); ok {
			t.Fatal("expired value returned")
		}
	}
	if len(a.order) != len(a.cache) || len(a.order) > 256 {
		t.Fatal("expired entries grew the cache order")
	}
}

func TestResolvePublicRejectsMixedDNSAnswers(t *testing.T) {
	a := newLinkPreviewAPI(http.NotFoundHandler())
	a.lookup = func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("127.0.0.1")}}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := a.resolvePublic(ctx, "example.com"); err == nil {
		t.Fatal("mixed public/private DNS response accepted")
	}
}

func TestFetchDialsValidatedIPAndRejectsPrivateRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/large" {
			_, _ = w.Write([]byte(strings.Repeat("x", linkPreviewBodyLimit+1)))
			return
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://private.example/secret", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(`<meta property="og:title" content="ok">`))
	}))
	defer server.Close()
	serverAddress := strings.TrimPrefix(server.URL, "http://")

	a := newLinkPreviewAPI(http.NotFoundHandler())
	a.lookup = func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host == "private.example" {
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
		}
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	}
	var dialTarget string
	dialer := &net.Dialer{}
	a.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialTarget = address
		return dialer.DialContext(ctx, network, serverAddress)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	target, _ := url.Parse("http://public.example/page")
	value, ok := a.fetch(ctx, target)
	if !ok || value.Title != "ok" || !strings.HasPrefix(dialTarget, "8.8.8.8:") {
		t.Fatalf("fetch=%#v ok=%v dial=%q", value, ok, dialTarget)
	}

	target, _ = url.Parse("http://public.example/redirect")
	if _, ok := a.fetch(ctx, target); ok {
		t.Fatal("redirect to private DNS address followed")
	}
	target, _ = url.Parse("http://public.example/large")
	if _, ok := a.fetch(ctx, target); ok {
		t.Fatal("oversized response accepted")
	}
}
