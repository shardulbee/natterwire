package main

import (
	"bytes"
	"container/list"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"reflect"
	"sync"

	"github.com/gen2brain/heic"
	"github.com/rwcarlsen/goexif/exif"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

const mediaByteLimit = 32 << 20

var errMediaNotFound = errors.New("media not found")
var errMediaChanged = errors.New("media changed")
var errMediaTooLarge = errors.New("media too large")

func init() { heic.ForceWasmMode = true }

type mediaResponse struct {
	body        []byte
	contentType string
}
type cacheEntry struct {
	id, version string
	response    mediaResponse
}
type attachmentMedia struct {
	mu            sync.Mutex
	budget, bytes int
	entries       *list.List
}

func newAttachmentMedia(budget int) *attachmentMedia {
	if budget < 0 {
		budget = 0
	}
	return &attachmentMedia{budget: budget, entries: list.New()}
}

func stamp(path string) (string, int64, bool) {
	i, err := os.Stat(path)
	if err != nil || !i.Mode().IsRegular() {
		return "", 0, false
	}
	identity := fmt.Sprintf("%d:%d", i.Size(), i.ModTime().UnixNano())
	// Linux and Darwin name ctime differently. Never include atime: merely
	// reading an image must not invalidate the version advertised by metadata.
	stat := reflect.Indirect(reflect.ValueOf(i.Sys()))
	if stat.IsValid() && stat.Kind() == reflect.Struct {
		for _, field := range []string{"Dev", "Ino", "Ctim", "Ctimespec"} {
			if value := stat.FieldByName(field); value.IsValid() {
				identity += fmt.Sprintf(":%v", value.Interface())
			}
		}
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(identity))), i.Size(), true
}

func readLimited(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errMediaNotFound
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, errMediaNotFound
	}
	if int64(len(b)) > limit {
		return nil, errMediaTooLarge
	}
	return b, nil
}

func (m *attachmentMedia) populate(a Attachment, metadataOnly bool) Attachment {
	v := "missing"
	a.Version = &v
	version, _, ok := stamp(a.path)
	if !ok {
		return a
	}
	a.Version = &version
	if cfg, orientation, err := imageInfo(a.path); err == nil {
		w, h := cfg.Width, cfg.Height
		if orientation >= 5 && orientation <= 8 {
			w, h = h, w
		}
		a.Width, a.Height = &w, &h
	}
	if metadataOnly {
		return a
	}
	b, err := readLimited(a.path, 10<<20)
	if err != nil {
		return a
	}
	s := base64.StdEncoding.EncodeToString(b)
	a.DataBase64 = &s
	if r, err := m.response(*a.MediaID, a.path, version); err == nil && !bytes.Equal(r.body, b) {
		display := base64.StdEncoding.EncodeToString(r.body)
		a.DisplayDataBase64 = &display
	}
	return a
}

func imageInfo(path string) (image.Config, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return image.Config{}, 1, err
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err == nil {
		o := 1
		if format == "jpeg" {
			_, _ = f.Seek(0, 0)
			if x, e := exif.Decode(f); e == nil {
				if tag, e := x.Get(exif.Orientation); e == nil {
					o, _ = tag.Int(0)
				}
			}
		}
		return cfg, o, nil
	}
	_, _ = f.Seek(0, 0)
	cfg, err = heic.DecodeConfig(f)
	return cfg, 1, err // HEIC decoder reports transformed dimensions.
}

func (m *attachmentMedia) response(id, path, requested string) (mediaResponse, error) {
	version, size, ok := stamp(path)
	if !ok {
		m.invalidate(id)
		return mediaResponse{}, errMediaNotFound
	}
	m.mu.Lock()
	for e := m.entries.Front(); e != nil; {
		next := e.Next()
		x := e.Value.(cacheEntry)
		if x.id == id && x.version != version {
			m.remove(e)
		}
		e = next
	}
	if requested != version {
		m.mu.Unlock()
		return mediaResponse{}, errMediaChanged
	}
	for e := m.entries.Front(); e != nil; e = e.Next() {
		x := e.Value.(cacheEntry)
		if x.id == id && x.version == version {
			m.entries.MoveToBack(e)
			m.mu.Unlock()
			if v, _, ok := stamp(path); !ok || v != version {
				return mediaResponse{}, errMediaChanged
			}
			return x.response, nil
		}
	}
	m.mu.Unlock()
	if size > mediaByteLimit {
		return mediaResponse{}, errMediaTooLarge
	}
	b, err := readLimited(path, mediaByteLimit)
	if err != nil {
		return mediaResponse{}, err
	}
	r, err := displayResponse(b)
	if err != nil {
		return mediaResponse{}, err
	}
	if v, _, ok := stamp(path); !ok || v != version {
		return mediaResponse{}, errMediaChanged
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for e := m.entries.Front(); e != nil; {
		next := e.Next()
		if e.Value.(cacheEntry).id == id {
			m.remove(e)
		}
		e = next
	}
	if len(r.body) <= m.budget {
		for m.bytes > m.budget-len(r.body) && m.entries.Len() > 0 {
			m.remove(m.entries.Front())
		}
		m.entries.PushBack(cacheEntry{id, version, r})
		m.bytes += len(r.body)
	}
	return r, nil
}

func displayResponse(b []byte) (mediaResponse, error) {
	if len(b) >= 12 && string(b[4:8]) == "ftyp" {
		cfg, err := heic.DecodeConfig(bytes.NewReader(b))
		if err != nil {
			return mediaResponse{}, errMediaNotFound
		}
		if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 32_000_000/cfg.Height {
			return mediaResponse{}, errMediaTooLarge
		}
		img, err := heic.Decode(bytes.NewReader(b))
		if err != nil {
			return mediaResponse{}, errMediaNotFound
		}
		return encodeJPEG(img)
	}
	ct := http.DetectContentType(b)
	allowed := map[string]bool{"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true, "image/tiff": true, "image/bmp": true}
	if !allowed[ct] {
		return mediaResponse{}, errMediaNotFound
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return mediaResponse{}, errMediaNotFound
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 32_000_000/cfg.Height {
		return mediaResponse{}, errMediaTooLarge
	}
	if ct == "image/jpeg" {
		x, e := exif.Decode(bytes.NewReader(b))
		if e == nil {
			tag, e := x.Get(exif.Orientation)
			if e == nil {
				o, _ := tag.Int(0)
				if o != 1 {
					img, e := jpeg.Decode(bytes.NewReader(b))
					if e != nil {
						return mediaResponse{}, errMediaNotFound
					}
					return encodeJPEG(orient(img, o))
				}
			}
		}
	}
	return mediaResponse{b, ct}, nil
}

func encodeJPEG(img image.Image) (mediaResponse, error) {
	if img.Bounds().Dx() <= 0 || img.Bounds().Dy() <= 0 || img.Bounds().Dx() > 32_000_000/img.Bounds().Dy() {
		return mediaResponse{}, errMediaTooLarge
	}
	var b bytes.Buffer
	if jpeg.Encode(&b, img, &jpeg.Options{Quality: 100}) != nil {
		return mediaResponse{}, errMediaNotFound
	}
	if b.Len() > mediaByteLimit {
		return mediaResponse{}, errMediaTooLarge
	}
	return mediaResponse{b.Bytes(), "image/jpeg"}, nil
}
func orient(src image.Image, o int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if o >= 5 && o <= 8 {
		w, h = h, w
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx, sy := x, y
			switch o {
			case 2:
				sx = w - 1 - x
			case 3:
				sx = w - 1 - x
				sy = h - 1 - y
			case 4:
				sy = h - 1 - y
			case 5:
				sx = y
				sy = x
			case 6:
				sx = y
				sy = b.Dy() - 1 - x
			case 7:
				sx = b.Dx() - 1 - y
				sy = b.Dy() - 1 - x
			case 8:
				sx = b.Dx() - 1 - y
				sy = x
			}
			dst.Set(x, y, src.At(b.Min.X+sx, b.Min.Y+sy))
		}
	}
	return dst
}
func (m *attachmentMedia) remove(e *list.Element) {
	m.bytes -= len(e.Value.(cacheEntry).response.body)
	m.entries.Remove(e)
}
func (m *attachmentMedia) invalidate(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for e := m.entries.Front(); e != nil; {
		n := e.Next()
		if e.Value.(cacheEntry).id == id {
			m.remove(e)
		}
		e = n
	}
}
func (m *attachmentMedia) cachedBytes() int { m.mu.Lock(); defer m.mu.Unlock(); return m.bytes }

// Alternate display bytes are needed for HEIC and oriented JPEG.
func displayImage(data []byte) *string {
	r, e := displayResponse(data)
	if e != nil || bytes.Equal(r.body, data) {
		return nil
	}
	s := base64.StdEncoding.EncodeToString(r.body)
	return &s
}
