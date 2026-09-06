package main

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"

	_ "golang.org/x/image/webp"
)

type attachment struct {
	ID, Filename, MimeType, DataBase64 string
	preview                            image.Image
}

// Decode on the HTTP worker, not the input loop. Keep only a small thumbnail
// after decoding, and reject oversized inputs before allocating pixel buffers.
func decodePreview(a attachment) image.Image {
	if (a.MimeType != "" && !strings.HasPrefix(a.MimeType, "image/")) || len(a.DataBase64) > base64.StdEncoding.EncodedLen(10<<20) {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(a.DataBase64)
	if err != nil {
		return nil
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 32_000_000 {
		return nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	scale := max(1, max(float64(config.Width)/48, float64(config.Height)/24))
	w, h := max(1, int(float64(config.Width)/scale)), max(1, int(float64(config.Height)/scale))
	thumb := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			thumb.Set(x, y, src.At(src.Bounds().Min.X+x*config.Width/w, src.Bounds().Min.Y+y*config.Height/h))
		}
	}
	return thumb
}

func (a attachment) label() string {
	name := a.Filename
	if name == "" {
		name = a.MimeType
	}
	if name == "" {
		name = "unnamed file"
	}
	if a.preview != nil || strings.HasPrefix(a.MimeType, "image/") {
		if a.preview == nil {
			return "[Image preview unavailable: " + name + "]"
		}
		return "[Image: " + name + "]"
	}
	return "[Attachment: " + name + "]"
}
