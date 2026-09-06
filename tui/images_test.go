package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.rockorager.dev/vaxis"
)

func TestImagePageAndRefresh(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 96, 48))
	img.Set(0, 0, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	a := attachment{ID: "image", Filename: "photo\n.png", MimeType: "image/png", DataBase64: base64.StdEncoding.EncodeToString(buf.Bytes())}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(page{Items: []item{{ID: "1", Text: "\ufffc", Attachments: []attachment{a}}}})
	}))
	defer server.Close()
	p, err := getPage(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	preview := p.Items[0].Attachments[0]
	if preview.Filename != "photo .png" || preview.preview == nil || preview.preview.Bounds().Size() != image.Pt(48, 24) {
		t.Fatalf("missing or oversized thumbnail: %+v", preview)
	}
	if got := color.NRGBAModel.Convert(preview.preview.At(0, 0)); got != (color.NRGBA{R: 255, A: 255}) {
		t.Fatalf("image pixels lost: %v", got)
	}
	next, err := getPage(context.Background(), server.Client(), server.URL)
	if err != nil || p.merge(next, false) {
		t.Fatalf("unchanged media poll dirtied cache: %v", err)
	}
	next.Items[0].Attachments[0].DataBase64 = ""
	if !p.merge(next, false) {
		t.Fatal("attachment-only edit was ignored")
	}
}

func TestPreviewFallbacks(t *testing.T) {
	for _, a := range []attachment{
		{MimeType: "image/jpeg"},
		{MimeType: "image/png", DataBase64: "not base64"},
		{MimeType: "image/heic", DataBase64: "aGVsbG8="},
		{MimeType: "image/png", DataBase64: strings.Repeat("A", base64.StdEncoding.EncodedLen(10<<20)+1)},
		{MimeType: "application/pdf", DataBase64: "aGVsbG8="},
	} {
		if decodePreview(a) != nil || !strings.Contains(a.label(), ": ") {
			t.Errorf("missing fallback for %q", a.MimeType)
		}
	}
}

func TestImageLayout(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	attachments := []attachment{
		{Filename: "one.png", MimeType: "image/png", preview: image.NewNRGBA(image.Rect(0, 0, 48, 24))},
		{Filename: "two.heic", MimeType: "image/heic"},
	}
	messages := []item{{ID: "1", Text: "\ufffcCaption", Attachments: attachments}}
	l := layout{dirty: true, following: true}
	vx := &vaxis.Vaxis{}
	l.prepare(messages, 60, 5, unicodeWidth, vx)
	if l.rows[1].text != "Caption" || l.rows[2].text != "[Image: one.png]" || l.rows[15].text != "[Image preview unavailable: two.heic]" {
		t.Fatalf("lost caption or attachment labels: %+v", l.rows)
	}
	if l.rows[3].image == nil || l.rows[14].imageRow != 11 {
		t.Fatal("image did not reserve twelve scrollable rows")
	}
	l.scroll(-5)
	anchor := l.rows[l.top]
	l.dirty = true
	l.prepare(append([]item{{ID: "2", Text: "new"}}, messages...), 60, 5, unicodeWidth, vx)
	if l.rows[l.top].id != anchor.id || l.rows[l.top].imageRow != anchor.imageRow {
		t.Fatal("refresh moved partially visible image")
	}
	l.prepare(messages, 20, 5, unicodeWidth, vx)
	if w, h := l.rows[3].image.CellSize(); w > 20 || h > 12 {
		t.Fatal("resize exceeded the transcript")
	}
	t.Setenv("NO_COLOR", "1")
	l.dirty = true
	l.prepare(messages, 60, 5, unicodeWidth, vx)
	for _, r := range l.rows {
		if r.image != nil {
			t.Fatal("NO_COLOR should retain labels without colored previews")
		}
	}
}
