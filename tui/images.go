package main

import (
	"bytes"
	"encoding/base64"
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
	ID, Filename, MimeType, DataBase64 string
	DisplayDataBase64                  string
	preview                            image.Image
}

// Decode the full image on the HTTP worker, not the input loop. The Mac supplies
// a full-resolution JPEG for HEIC; original attachment bytes remain untouched.
func decodePreview(a attachment) image.Image {
	encoded := a.DataBase64
	if a.DisplayDataBase64 != "" {
		encoded = a.DisplayDataBase64
	}
	if (a.MimeType != "" && !strings.HasPrefix(a.MimeType, "image/")) || len(encoded) > base64.StdEncoding.EncodedLen(64<<20) {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
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
	if a.preview != nil || strings.HasPrefix(a.MimeType, "image/") || strings.HasSuffix(strings.ToLower(name), ".pluginpayloadattachment") {
		return "[Image: " + name + "]"
	}
	return "[Attachment: " + name + "]"
}

func fitImage(src image.Image, width, height int) *image.NRGBA {
	b := src.Bounds()
	scale := max(1, max(float64(b.Dx())/float64(width), float64(b.Dy())/float64(height)))
	w, h := max(1, int(float64(b.Dx())/scale)), max(1, int(float64(b.Dy())/scale))
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
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
	pixels *image.NRGBA
	cell   image.Point
	rows   int
	crop   image.Rectangle
	kitty  *vaxis.KittyImage
}

func newInlineImage(src image.Image, width, height int, cell image.Point) *inlineImage {
	// Fit to the available terminal area, not an arbitrary thumbnail size.
	pixels := fitImage(src, width*cell.X, max(1, height-3)*cell.Y)
	return &inlineImage{pixels: pixels, cell: cell, rows: (pixels.Bounds().Dy() + cell.Y - 1) / cell.Y}
}

func (p *inlineImage) destroy(vx *vaxis.Vaxis) {
	if p.kitty != nil {
		vx.RemoveImage(p.kitty)
		p.kitty.Destroy()
		p.kitty = nil
	}
}

func (p *inlineImage) draw(vx *vaxis.Vaxis, win vaxis.Window, firstRow, rows int) {
	b := p.pixels.Bounds()
	crop := image.Rect(0, firstRow*p.cell.Y, b.Dx(), min(b.Dy(), (firstRow+rows)*p.cell.Y))
	if p.kitty == nil || p.crop != crop {
		p.destroy(vx)
		// Native Kitty placements ignore Window clipping. Upload only the visible
		// pixels, rebased to zero origin, so images cannot overlap the header/draft.
		visible := image.NewNRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
		draw.Draw(visible, visible.Bounds(), p.pixels, crop.Min, draw.Src)
		p.kitty, p.crop = vx.NewKittyGraphic(visible), crop
		p.kitty.Resize((crop.Dx()+p.cell.X-1)/p.cell.X, rows)
	}
	// Each object is resized once. Obsolete encodings are never drawn/uploaded.
	p.kitty.Draw(win)
}
