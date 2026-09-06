package main

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"

	"go.rockorager.dev/vaxis"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

type attachment struct {
	ID, Filename, MimeType string
	MediaID, Version       string
	Width, Height          int
}

// Called only on the media worker. HEIC is served as full-resolution JPEG.
func decodeImage(data []byte) image.Image {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 32_000_000 {
		return nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	return src
}

func (a attachment) label() string {
	name := a.Filename
	if name == "" {
		name = a.MimeType
	}
	if name == "" {
		name = "unnamed file"
	}
	if strings.HasPrefix(a.MimeType, "image/") || strings.HasSuffix(strings.ToLower(name), ".pluginpayloadattachment") {
		return "[Image: " + name + "]"
	}
	return "[Attachment: " + name + "]"
}

func fitImage(src image.Image, width, height int) *image.NRGBA {
	b := src.Bounds()
	size := fittedSize(b.Size(), image.Pt(width, height))
	dst := image.NewNRGBA(image.Rectangle{Max: size})
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}

func fittedSize(src, available image.Point) image.Point {
	scale := max(1, max(float64(src.X)/float64(max(1, available.X)), float64(src.Y)/float64(max(1, available.Y))))
	return image.Pt(max(1, int(float64(src.X)/scale)), max(1, int(float64(src.Y)/scale)))
}

func kittyCell(vx *vaxis.Vaxis) image.Point {
	s := vx.Size()
	if !vx.CanKittyGraphics() || s.Cols <= 0 || s.Rows <= 0 {
		return image.Point{}
	}
	cell := image.Pt(s.XPixel/s.Cols, s.YPixel/s.Rows)
	if cell.X <= 0 || cell.Y <= 0 {
		return image.Point{}
	}
	return cell
}

type inlineImage struct {
	attachment attachment
	size       image.Point
	cell       image.Point
	rows       int
}

func newInlineImage(a attachment, width, height int, cell image.Point) *inlineImage {
	// Layout only computes geometry. The media worker handles pixels.
	size := fittedSize(image.Pt(a.Width, a.Height), image.Pt(width*cell.X, max(1, height-3)*cell.Y))
	return &inlineImage{attachment: a, size: size, cell: cell, rows: (size.Y + cell.Y - 1) / cell.Y}
}

type imageKey struct {
	id, version string
	size, cell  image.Point
}

func (p *inlineImage) key() imageKey {
	return imageKey{p.attachment.MediaID, p.attachment.Version, p.size, p.cell}
}
