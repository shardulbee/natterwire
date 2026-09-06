package vaxis

import (
	"bytes"
	"image"
	"strings"
	"testing"
	"time"
)

func cropTestImage(t *testing.T) (*Vaxis, *KittyImage, Window) {
	t.Helper()
	vx := &Vaxis{
		queue:      make(chan Event),
		winSize:    Resize{Cols: 80, Rows: 24, XPixel: 800, YPixel: 480},
		screenNext: newScreen(),
		screenLast: newScreen(),
	}
	vx.tw = &writer{buf: new(bytes.Buffer), vx: vx}
	k := vx.NewKittyGraphic(image.NewRGBA(image.Rect(0, 0, 35, 65)))
	k.Resize(4, 4)
	// An unbuffered event queue holds the encoder at PostEventBlocking.
	// Readiness must be published before that send can complete.
	deadline := time.Now().Add(time.Second)
	for atomicLoad(&k.encoding) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	ready := !atomicLoad(&k.encoding)
	select {
	case <-vx.queue:
	case <-time.After(time.Second):
		t.Fatal("missing resize redraw")
	}
	if !ready {
		t.Fatal("encoder was not ready before posting redraw")
	}
	return vx, k, Window{Vx: vx, Column: 2, Row: 3, Width: 4, Height: 4}
}

func TestKittyCropFrames(t *testing.T) {
	vx, k, win := cropTestImage(t)
	frame := func(src image.Rectangle) string {
		vx.graphicsNext = nil
		vx.tw.buf.Reset()
		k.DrawCrop(win, src)
		vx.render()
		return vx.tw.buf.String()
	}
	first := frame(image.Rect(0, 0, 35, 25))
	if strings.Count(first, "f=100,") != 1 || !strings.Contains(first, ",C=1,x=0,y=0,w=35,h=25\x1b\\") {
		t.Fatalf("first frame = %q", first)
	}
	if strings.Contains(first, ",c=") || strings.Contains(first, ",r=") {
		t.Fatalf("crop stretches partial cells: %q", first)
	}
	p := vx.graphicsLast[0]
	if p.w != 4 || p.h != 2 {
		t.Fatalf("placement size = %dx%d, want 4x2", p.w, p.h)
	}
	if got := frame(image.Rect(0, 0, 35, 25)); strings.Contains(got, "\x1b_G") {
		t.Fatalf("unchanged placement emitted graphics: %q", got)
	}
	changed := frame(image.Rect(0, 20, 35, 45))
	if strings.Contains(changed, "f=100,") || !strings.Contains(changed, "a=p,") || !strings.Contains(changed, "x=0,y=20,w=35,h=25") {
		t.Fatalf("changed crop = %q", changed)
	}
	if samePlacement(p, vx.graphicsLast[0]) {
		t.Fatal("changed same-position crop compares equal")
	}
	removed := frame(image.Rectangle{})
	if !strings.Contains(removed, "a=d,d=i,") || strings.Contains(removed, "d=I") || strings.Contains(removed, "a=p,") {
		t.Fatalf("removal must delete only the placement: %q", removed)
	}
	if got := frame(image.Rect(0, 0, 35, 25)); strings.Contains(got, "f=100,") || !strings.Contains(got, "a=p,") {
		t.Fatalf("reappearing crop must reuse upload: %q", got)
	}
}

func TestKittyCropBounds(t *testing.T) {
	vx, k, win := cropTestImage(t)
	for _, tc := range []struct {
		name          string
		src           image.Rectangle
		width, height int
		want          image.Rectangle
		w, h          int
	}{
		{"partial bottom", image.Rect(0, 40, 35, 65), 4, 4, image.Rect(0, 40, 35, 65), 4, 2},
		{"window", image.Rect(5, 10, 35, 65), 2, 2, image.Rect(5, 10, 25, 50), 2, 2},
		{"image", image.Rect(-5, -10, 100, 100), 20, 20, image.Rect(0, 0, 35, 65), 4, 4},
		{"outside image", image.Rect(40, 70, 50, 80), 4, 4, image.Rectangle{}, 0, 0},
		{"offscreen window", image.Rect(0, 0, 35, 65), 4, 0, image.Rectangle{}, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vx.graphicsNext = nil
			win.Width, win.Height = tc.width, tc.height
			k.DrawCrop(win, tc.src)
			if tc.want.Empty() {
				if len(vx.graphicsNext) != 0 {
					t.Fatal("empty crop placed image")
				}
				return
			}
			if len(vx.graphicsNext) != 1 {
				t.Fatal("missing crop")
			}
			p := vx.graphicsNext[0]
			if p.crop != tc.want || p.w != tc.w || p.h != tc.h {
				t.Fatalf("crop %v size %dx%d, want %v size %dx%d", p.crop, p.w, p.h, tc.want, tc.w, tc.h)
			}
		})
	}
}

func TestKittyDrawUnchanged(t *testing.T) {
	vx, k, win := cropTestImage(t)
	k.Draw(win)
	p := vx.graphicsNext[0]
	var out bytes.Buffer
	p.writeTo(&out)
	if !strings.HasSuffix(out.String(), "\x1b_Ga=p,i=1,p=131075,C=1\x1b\\") || p.w != 4 || p.h != 4 || !p.crop.Empty() {
		t.Fatalf("Draw changed: %q, placement %#v", out.String(), p)
	}
	k.Draw(win)
	if !samePlacement(p, vx.graphicsNext[1]) {
		t.Fatal("identical Draw placements differ")
	}
}
