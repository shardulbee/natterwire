package main

import (
	"bytes"
	"database/sql"
	"encoding/hex"
	"image"
	"image/jpeg"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestMetadataBinaryMedia(t *testing.T) {
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, image.NewNRGBA(image.Rect(0, 0, 12, 8))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(path, pngBytes.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	d := fixture(t, func(db *sql.DB) {
		if _, err := db.Exec("UPDATE attachment SET filename=?", path); err != nil {
			t.Fatal(err)
		}
	})
	target := "/messages/" + opaque("iMessage;-;alex@example.invalid") + "?limit=1&media=metadata"
	a := get[Message](t, d, target).Items[0].Attachments[0]
	if a.DataBase64 != nil || a.DisplayDataBase64 != nil || a.Width == nil || *a.Width != 12 || *a.Height != 8 || d.media.cachedBytes() != 0 {
		t.Fatalf("%+v", a)
	}
	request := func(target string, status int) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		d.ServeHTTP(w, httptest.NewRequest("GET", target, nil))
		if w.Code != status {
			t.Fatalf("%s: %d %s", target, w.Code, w.Body.String())
		}
		wantCache := "no-store"
		if status == 200 && w.Header().Get("Content-Type") == "image/png" {
			wantCache = "private, max-age=31536000, immutable"
		}
		if got := w.Header().Get("Cache-Control"); got != wantCache {
			t.Fatalf("%s: Cache-Control = %q, want %q", target, got, wantCache)
		}
		return w
	}
	mediaURL := "/attachments/" + *a.MediaID + "?version=" + *a.Version
	w := request(mediaURL, 200)
	if w.Header().Get("Content-Type") != "image/png" || !bytes.Equal(w.Body.Bytes(), pngBytes.Bytes()) {
		t.Fatal("binary response changed")
	}
	request(mediaURL, 200)
	if d.media.cachedBytes() != pngBytes.Len() {
		t.Fatal("cache")
	}
	request("/attachments/"+*a.MediaID, 409)
	request("/attachments/"+opaque(path)+"?version="+*a.Version, 404)
	request("/attachments/"+opaque("attachment:01")+"?version="+*a.Version, 404)
	cache := newAttachmentMedia(pngBytes.Len())
	if _, err := cache.response("a", path, *a.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := cache.response("b", path, *a.Version); err != nil {
		t.Fatal(err)
	}
	if cache.cachedBytes() != pngBytes.Len() {
		t.Fatal("budget")
	}
	if err := os.WriteFile(path, append(pngBytes.Bytes(), 0), 0600); err != nil {
		t.Fatal(err)
	}
	request(mediaURL, 409)
	if d.media.cachedBytes() != 0 {
		t.Fatal("stale cache retained")
	}
	a = get[Message](t, d, target).Items[0].Attachments[0]
	request("/attachments/"+*a.MediaID+"?version="+*a.Version, 200)
	d.mediaSlots <- struct{}{}
	d.mediaSlots <- struct{}{}
	request(mediaURL, 503)
	request(target, 200)
	request("/chats", 200)
	<-d.mediaSlots
	<-d.mediaSlots
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() { defer wg.Done(); get[Message](t, d, target) }()
	}
	wg.Wait()
	f, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(mediaByteLimit + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	a = get[Message](t, d, target).Items[0].Attachments[0]
	request("/attachments/"+*a.MediaID+"?version="+*a.Version, 413)
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	request(mediaURL, 404)
	a = get[Message](t, d, target).Items[0].Attachments[0]
	if *a.Version != "missing" {
		t.Fatal("missing version")
	}
}

func TestJPEGOrientation(t *testing.T) {
	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, image.NewNRGBA(image.Rect(0, 0, 120, 80)), nil); err != nil {
		t.Fatal(err)
	}
	// APP1 EXIF with a single little-endian TIFF Orientation=6 entry.
	app1, err := hex.DecodeString("ffe1002245786966000049492a0008000000010012010300010000000600000000000000")
	if err != nil {
		t.Fatal(err)
	}
	data := append(append(append([]byte{}, raw.Bytes()[:2]...), app1...), raw.Bytes()[2:]...)
	path := filepath.Join(t.TempDir(), "rotated.jpg")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, o, err := imageInfo(path)
	if err != nil || o != 6 || cfg.Width != 120 {
		t.Fatalf("%+v %d %v", cfg, o, err)
	}
	response, err := displayResponse(data)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err = jpeg.DecodeConfig(bytes.NewReader(response.body))
	if err != nil || cfg.Width != 80 || cfg.Height != 120 {
		t.Fatalf("%+v %v", cfg, err)
	}
}
