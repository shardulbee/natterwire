package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

const linkPreviewBodyLimit = 1 << 20

type linkPreview struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Image       string `json:"image"`
}

type previewCacheEntry struct {
	value   linkPreview
	expires time.Time
}

type linkPreviewAPI struct {
	next   http.Handler
	lookup func(context.Context, string) ([]net.IPAddr, error)
	dial   func(context.Context, string, string) (net.Conn, error)
	slots  chan struct{}

	mu    sync.Mutex
	cache map[string]previewCacheEntry
	order []string
}

func newLinkPreviewAPI(next http.Handler) *linkPreviewAPI {
	d := &net.Dialer{Timeout: 4 * time.Second}
	return &linkPreviewAPI{
		next:   next,
		lookup: net.DefaultResolver.LookupIPAddr,
		dial:   d.DialContext,
		slots:  make(chan struct{}, 4),
		cache:  make(map[string]previewCacheEntry),
	}
}

func (a *linkPreviewAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/link-preview" {
		a.next.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	reply := func(status int, value any) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(value)
	}
	if r.Method != http.MethodGet {
		reply(http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !sameOrigin(r) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		reply(http.StatusForbidden, map[string]string{"error": "cross-origin browser preview requests are not allowed"})
		return
	}
	u, err := parsePreviewURL(r.URL.Query().Get("url"))
	if err != nil {
		reply(http.StatusBadRequest, map[string]string{"error": "url must be an absolute public http(s) URL"})
		return
	}
	key := u.String()
	if value, ok := a.cached(key); ok {
		reply(http.StatusOK, value)
		return
	}
	select {
	case a.slots <- struct{}{}:
		defer func() { <-a.slots }()
	default:
		reply(http.StatusOK, linkPreview{})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	value, ok := a.fetch(ctx, u)
	a.store(key, value, ok)
	reply(http.StatusOK, value)
}

func parsePreviewURL(raw string) (*url.URL, error) {
	if len(raw) > 8192 {
		return nil, errors.New("URL too long")
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("unsafe URL")
	}
	if u.Hostname() == "" || strings.ContainsAny(u.Hostname(), "\x00\r\n") {
		return nil, errors.New("unsafe host")
	}
	return u, nil
}

func publicIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if addr.Is6() && !netip.MustParsePrefix("2000::/3").Contains(addr) {
		return false
	}
	// IsGlobalUnicast includes documentation, benchmarking, carrier NAT, and
	// other special-purpose ranges which must not be reachable through this API.
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return addr.IsGlobalUnicast()
}

var nonPublicPrefixes = func() []netip.Prefix {
	raw := []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
		"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
		"2001::/23", "2001:db8::/32", "2002::/16", "3fff::/20",
	}
	prefixes := make([]netip.Prefix, 0, len(raw))
	for _, value := range raw {
		prefixes = append(prefixes, netip.MustParsePrefix(value))
	}
	return prefixes
}()

func (a *linkPreviewAPI) resolvePublic(ctx context.Context, host string) ([]net.IPAddr, error) {
	addresses, err := a.lookup(ctx, host)
	if err != nil || len(addresses) == 0 {
		return nil, errors.New("host unavailable")
	}
	for _, address := range addresses {
		if !publicIP(address.IP) {
			return nil, errors.New("host resolves to a non-public address")
		}
	}
	return addresses, nil
}

func (a *linkPreviewAPI) client() *http.Client {
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			addresses, err := a.resolvePublic(ctx, host)
			if err != nil {
				return nil, err
			}
			// Connect to the validated result, not the hostname, to prevent DNS rebinding.
			return a.dial(ctx, network, net.JoinHostPort(addresses[0].IP.String(), port))
		},
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: 4 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if _, err := parsePreviewURL(req.URL.String()); err != nil {
				return err
			}
			_, err := a.resolvePublic(req.Context(), req.URL.Hostname())
			return err
		},
	}
}

func (a *linkPreviewAPI) fetch(ctx context.Context, target *url.URL) (linkPreview, bool) {
	if _, err := a.resolvePublic(ctx, target.Hostname()); err != nil {
		return linkPreview{}, false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return linkPreview{}, false
	}
	req.Header.Set("User-Agent", "Natterwire-Link-Preview/1.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	response, err := a.client().Do(req)
	if err != nil {
		return linkPreview{}, false
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return linkPreview{}, false
	}
	reader := io.LimitReader(response.Body, linkPreviewBodyLimit+1)
	data, err := io.ReadAll(reader)
	if err != nil || len(data) > linkPreviewBodyLimit {
		return linkPreview{}, false
	}
	value := parseLinkPreview(strings.NewReader(string(data)), response.Request.URL)
	if value.Image != "" {
		image, err := parsePreviewURL(value.Image)
		if err != nil {
			value.Image = ""
		} else if _, err = a.resolvePublic(ctx, image.Hostname()); err != nil {
			value.Image = ""
		} else {
			value.Image = image.String()
		}
	}
	return value, true
}

func parseLinkPreview(r io.Reader, base *url.URL) linkPreview {
	doc, err := html.Parse(r)
	if err != nil {
		return linkPreview{}
	}
	values := make(map[string]string)
	var title string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "meta" {
			var key, content string
			for _, attr := range n.Attr {
				switch attr.Key {
				case "property", "name":
					key = strings.ToLower(strings.TrimSpace(attr.Val))
				case "content":
					content = strings.TrimSpace(attr.Val)
				}
			}
			if content != "" && values[key] == "" {
				values[key] = content
			}
		}
		if n.Type == html.ElementNode && n.Data == "title" && title == "" && n.FirstChild != nil {
			title = strings.TrimSpace(textContent(n))
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	value := linkPreview{
		Title:       first(values["og:title"], values["twitter:title"], title),
		Description: first(values["og:description"], values["twitter:description"]),
		Image:       first(values["og:image"], values["twitter:image"], values["twitter:image:src"]),
	}
	if value.Image != "" {
		if image, err := base.Parse(value.Image); err == nil {
			value.Image = image.String()
		} else {
			value.Image = ""
		}
	}
	return value
}

func textContent(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			b.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return b.String()
}

func first(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (a *linkPreviewAPI) cached(key string) (linkPreview, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	entry, ok := a.cache[key]
	if !ok || time.Now().After(entry.expires) {
		return linkPreview{}, false
	}
	return entry.value, true
}

func (a *linkPreviewAPI) store(key string, value linkPreview, success bool) {
	ttl := 5 * time.Minute
	if success {
		ttl = time.Hour
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.cache[key]; !exists {
		if len(a.cache) >= 256 {
			delete(a.cache, a.order[0])
			a.order = a.order[1:]
		}
		a.order = append(a.order, key)
	}
	a.cache[key] = previewCacheEntry{value: value, expires: time.Now().Add(ttl)}
}
