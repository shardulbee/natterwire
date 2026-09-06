package main

import (
	"context"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.rockorager.dev/vaxis"
)

const sourceBudget = 128 << 20
const displayBudget = 64 << 20

type sourceKey struct{ id, version string }
type sourceImage struct {
	pixels image.Image
	used   uint64
	bytes  int
}
type displayImage struct {
	pixels *image.NRGBA
	kitty  *vaxis.KittyImage
	used   uint64
	bytes  int
	retry  time.Time
}
type mediaResult struct {
	key    imageKey
	pixels *image.NRGBA
}

// The event loop owns displays; a single media worker owns decoded sources.
// Neither cache keeps base64 or duplicate downloaded bytes after decoding.
type mediaCache struct {
	displays           map[imageKey]*displayImage
	sources            map[sourceKey]sourceImage
	results            chan mediaResult
	busy               bool
	tick, sourceTick   uint64
	bytes, sourceBytes int
}

func newMediaCache() *mediaCache {
	return &mediaCache{displays: make(map[imageKey]*displayImage), sources: make(map[sourceKey]sourceImage), results: make(chan mediaResult, 1)}
}

func (m *mediaCache) destroy(vx *vaxis.Vaxis, d *displayImage) {
	if d.kitty != nil {
		vx.RemoveImage(d.kitty)
		d.kitty.Destroy()
	}
}

func (m *mediaCache) close(vx *vaxis.Vaxis) {
	for _, d := range m.displays {
		m.destroy(vx, d)
	}
}

func (m *mediaCache) accept(vx *vaxis.Vaxis, r mediaResult) {
	m.busy = false
	if old := m.displays[r.key]; old != nil {
		m.destroy(vx, old)
		m.bytes -= old.bytes
	}
	m.tick++
	d := &displayImage{pixels: r.pixels, used: m.tick, retry: time.Now().Add(30 * time.Second)}
	if r.pixels != nil {
		// Account for local pixels and the terminal's uncompressed copy.
		d.bytes = len(r.pixels.Pix) * 2
	}
	m.displays[r.key] = d
	m.bytes += d.bytes
	for m.bytes > displayBudget || len(m.displays) > 256 {
		var oldest imageKey
		age := ^uint64(0)
		for k, v := range m.displays {
			if v.used < age {
				oldest, age = k, v.used
			}
		}
		v := m.displays[oldest]
		m.destroy(vx, v)
		m.bytes -= v.bytes
		delete(m.displays, oldest)
	}
}

func (m *mediaCache) draw(vx *vaxis.Vaxis, win vaxis.Window, p *inlineImage, firstRow, rows int) {
	d := m.displays[p.key()]
	if d == nil || d.pixels == nil {
		return
	}
	m.tick++
	d.used = m.tick
	if d.kitty == nil {
		d.kitty = vx.NewKittyGraphic(d.pixels)
		// Already fitted by the worker; Vaxis need not resample these pixels.
		d.kitty.Resize((p.size.X+p.cell.X-1)/p.cell.X, p.rows)
	}
	crop := image.Rect(0, firstRow*p.cell.Y, p.size.X, min(p.size.Y, (firstRow+rows)*p.cell.Y))
	d.kitty.DrawCrop(win, crop)
}

// Visible images first, then at most half a screen above/below. There is no
// unbounded request queue: switching chats changes the next job immediately.
func (m *mediaCache) pump(ctx context.Context, client *http.Client, base string, l *layout) {
	if m.busy || l == nil || l.cell.X <= 0 {
		return
	}
	seen := make(map[imageKey]bool)
	for _, span := range [][2]int{{l.top, l.top + l.height}, {max(0, l.top-l.height/2), l.top}, {l.top + l.height, l.top + l.height + l.height/2}} {
		for i := span[0]; i < min(len(l.rows), span[1]); i++ {
			p := l.rows[i].image
			if p == nil {
				continue
			}
			key := p.key()
			if seen[key] {
				continue
			}
			seen[key] = true
			if d := m.displays[key]; d != nil && (d.pixels != nil || time.Now().Before(d.retry)) {
				continue
			}
			m.busy = true
			go func() {
				pixels := m.load(ctx, client, base, key)
				select {
				case m.results <- mediaResult{key, pixels}:
				case <-ctx.Done():
				}
			}()
			return
		}
	}
}

func (m *mediaCache) load(ctx context.Context, client *http.Client, base string, key imageKey) *image.NRGBA {
	// Bound transient display work as well as retained cache memory.
	if key.size.X <= 0 || key.size.Y <= 0 || int64(key.size.X)*int64(key.size.Y)*8 > displayBudget {
		return nil
	}
	sk := sourceKey{key.id, key.version}
	source, ok := m.sources[sk]
	if !ok {
		data, err := fetchMedia(ctx, client, base, sk)
		if err != nil {
			return nil
		}
		source.pixels = decodeImage(data)
		if source.pixels == nil {
			return nil
		}
		// Conservative upper bound, including 16-bit and palette decoders.
		b := source.pixels.Bounds()
		source.bytes = b.Dx() * b.Dy() * 8
		for m.sourceBytes+source.bytes > sourceBudget && len(m.sources) > 0 {
			var oldest sourceKey
			age := ^uint64(0)
			for k, v := range m.sources {
				if v.used < age {
					oldest, age = k, v.used
				}
			}
			m.sourceBytes -= m.sources[oldest].bytes
			delete(m.sources, oldest)
		}
		if source.bytes <= sourceBudget {
			m.sourceBytes += source.bytes
		}
	}
	m.sourceTick++
	source.used = m.sourceTick
	if source.bytes <= sourceBudget {
		m.sources[sk] = source
	}
	return fitImage(source.pixels, key.size.X, key.size.Y)
}

func fetchMedia(ctx context.Context, client *http.Client, base string, key sourceKey) ([]byte, error) {
	target := strings.TrimRight(base, "/") + "/attachments/" + url.PathEscape(key.id) + "?" + url.Values{"version": {key.version}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("media HTTP %d", res.StatusCode)
	}
	const limit = 64 << 20
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errPageTooLarge
	}
	return data, nil
}
