package main

import (
	"fmt"
	"os"
	"strings"

	"go.rockorager.dev/vaxis"
)

func unicodeWidth(s string) int {
	w := 0
	it := vaxis.NewCharacterIterator(s)
	for ch, ok := it.Next(); ok; ch, ok = it.Next() {
		w += ch.Width
	}
	return w
}

// Some web terminals shape emoji without advertising Unicode mode. Measure two
// cursor advances before the first frame; Vaxis bounds each query to 50ms.
func terminalWidth(vx *vaxis.Vaxis) func(string) int {
	measure := vx.RenderedWidth
	w, _ := vx.Window().Size()
	forced := os.Getenv("VAXIS_FORCE_UNICODE") != "" || os.Getenv("VAXIS_FORCE_WCWIDTH") != "" || os.Getenv("VAXIS_FORCE_NOZWJ") != ""
	if w >= 40 && !forced && !vx.CanExplicitWidth() {
		if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
			fmt.Fprint(tty, "\x1b[1;16H❤️")
			_, heart := vx.CursorPosition()
			fmt.Fprint(tty, "\x1b[1;32H👩‍👩‍👧‍👦")
			_, family := vx.CursorPosition()
			fmt.Fprint(tty, "\x1b[H\x1b[2J")
			tty.Close()
			if heart == 17 && family == 33 {
				measure = unicodeWidth
			}
			if heart == 17 && family == 39 {
				measure = func(s string) int { return unicodeWidth(strings.ReplaceAll(s, "\u200d", "")) }
			}
		}
	}
	cache := make(map[string]int)
	return func(s string) int {
		if width, ok := cache[s]; ok {
			return width
		}
		width := measure(s)
		cache[s] = width
		return width
	}
}
