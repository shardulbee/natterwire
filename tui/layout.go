package main

import (
	"fmt"
	"image"
	"os"
	"slices"
	"strings"

	"go.rockorager.dev/vaxis"
	"go.rockorager.dev/vaxis/widgets/border"
)

var (
	accent        = style(6, 0, vaxis.AttrBold)
	muted         = style(8, 0, 0)
	selectedStyle = style(0, 6, vaxis.AttrBold)
	inactiveStyle = style(7, 8, vaxis.AttrBold)
)

func style(fg, bg uint8, attrs vaxis.AttributeMask) vaxis.Style {
	s := vaxis.Style{Attribute: attrs}
	if os.Getenv("NO_COLOR") == "" {
		s.Foreground = vaxis.IndexColor(fg)
		if bg != 0 {
			s.Background = vaxis.IndexColor(bg)
		}
	}
	return s
}

type row struct {
	text, id string
	offset   int
	style    vaxis.Style
	image    *inlineImage
	imageRow int
}

type layout struct {
	rows                            []row
	width, height, top, newMessages int
	head                            string
	dirty, following                bool
	cell                            image.Point
	blocks                          map[string]messageBlock
}

type messageBlock struct {
	item item
	rows []row
}

func (l *layout) bottom() {
	l.following, l.newMessages = true, 0
	l.top = max(0, len(l.rows)-l.height)
}

func (l *layout) scroll(n int) {
	l.top = min(max(0, l.top+n), max(0, len(l.rows)-l.height))
	l.following = l.top == max(0, len(l.rows)-l.height)
	if l.following {
		l.newMessages = 0
	}
}

// Byte offsets keep the same message text visible when new pages arrive or width changes.
func (l *layout) prepare(messages []item, width, height int, measure func(string) int, cell image.Point) {
	if l.dirty || l.width != width || l.cell != cell || (cell.Y > 0 && l.height != height) {
		anchor := row{}
		if !l.following && l.top < len(l.rows) {
			anchor = l.rows[l.top]
		}
		if !l.following && l.head != "" {
			for i, m := range messages {
				if m.ID == l.head {
					l.newMessages += i
					break
				}
			}
		}
		l.rows = l.rows[:0]
		oldBlocks := l.blocks
		if l.width != width || l.height != height || l.cell != cell {
			oldBlocks = nil
		}
		l.blocks = make(map[string]messageBlock, len(messages))
		l.head = ""
		if len(messages) > 0 {
			l.head = messages[0].ID
		}
		for i := len(messages) - 1; i >= 0; i-- {
			m := messages[i]
			if block, ok := oldBlocks[m.ID]; ok && equalItem(block.item, m) {
				l.rows = append(l.rows, block.rows...)
				l.blocks[m.ID] = block
				continue
			}
			startRow := len(l.rows)
			name, headerStyle := m.Sender, muted
			if name == "" {
				name = "Unknown sender"
			}
			if m.IsFromMe {
				name, headerStyle = "You", accent
			}
			date := m.SentAt
			if len(date) >= 16 && date[10] == 'T' {
				date = date[:10] + " " + date[11:16]
			}
			l.rows = append(l.rows, row{text: name + "  " + date, id: m.ID, style: headerStyle})
			text := m.Text
			if len(m.Attachments) > 0 {
				text = strings.ReplaceAll(text, "\ufffc", "")
			}
			if text == "" && len(m.Attachments) == 0 {
				text = "[Attachment or unavailable message text]"
			}
			for start := 0; start < len(text); {
				end, next, col, space := start, start, 0, -1
				it := vaxis.NewCharacterIterator(text[start:])
				for ch, ok := it.Next(); ok; ch, ok = it.Next() {
					if ch.Grapheme == "\n" {
						next = end + 1
						break
					}
					w := measure(ch.Grapheme)
					if col+w > width && end > start {
						next = end
						if space >= start {
							end, next = space, space+1
						}
						break
					}
					if ch.Grapheme == " " {
						space = end
					}
					end += len(ch.Grapheme)
					next, col = end, col+w
				}
				l.rows = append(l.rows, row{text: text[start:end], id: m.ID, offset: start + 1})
				start = next
			}
			offset := len(text) + 1
			for _, a := range m.Attachments {
				l.rows = append(l.rows, row{text: a.label(), id: m.ID, offset: offset, style: muted})
				offset++
				if a.MediaID != "" && a.Width > 0 && a.Height > 0 && cell.X > 0 && cell.Y > 0 {
					preview := newInlineImage(a, width, height, cell)
					for y := 0; y < preview.rows; y++ {
						l.rows = append(l.rows, row{id: m.ID, offset: offset, image: preview, imageRow: y})
						offset++
					}
				}
			}
			l.rows = append(l.rows, row{id: m.ID, offset: offset})
			l.blocks[m.ID] = messageBlock{m, slices.Clone(l.rows[startRow:])}
		}
		if anchor.id != "" {
			for i, r := range l.rows {
				if r.id == anchor.id {
					if r.offset > anchor.offset {
						break
					}
					l.top = i
				}
			}
		}
		l.width, l.cell, l.dirty = width, cell, false
	}
	l.height = height
	if l.following {
		l.bottom()
	}
	l.scroll(0)
}

// Preflight whole graphemes at the right edge, using the same widths as wrapping.
func line(win vaxis.Window, y int, text string, s vaxis.Style, measure func(string) int) {
	w, _ := win.Size()
	col := 0
	it := vaxis.NewCharacterIterator(text)
	for ch, ok := it.Next(); ok; ch, ok = it.Next() {
		ch.Width = measure(ch.Grapheme)
		if col+ch.Width > w {
			break
		}
		win.SetCell(col, y, vaxis.Cell{Character: ch, Style: s})
		col += ch.Width
	}
}

func (a *app) draw(vx *vaxis.Vaxis, measure func(string) int) {
	root := vx.Window()
	root.Clear()
	vx.HideCursor()
	w, h := root.Size()
	if w < 60 || h < 14 {
		line(root, 0, "Natterwire needs 60 columns x 14 rows.", accent, measure)
		line(root, 1, "Resize your terminal. Ctrl+C quits.", accent, measure)
		return
	}
	sw, focus := min(30, w/3), muted
	if a.mode == sidebar {
		focus = accent
	}
	side := border.Right(root.New(0, 0, sw, h-1), focus)
	title := " Chats"
	if a.demo {
		title += " / demo"
	}
	line(side, 0, title, focus, measure)
	visible := h - 3
	a.sidebarTop = min(a.sidebarTop, a.selected)
	if a.selected >= a.sidebarTop+visible {
		a.sidebarTop = a.selected - visible + 1
	}
	a.sidebarTop = min(a.sidebarTop, max(0, len(a.chats.Items)-visible))
	for i := a.sidebarTop; i < min(len(a.chats.Items), a.sidebarTop+visible); i++ {
		s := vaxis.Style{}
		if i == a.selected {
			s = inactiveStyle
			if a.mode == sidebar {
				s = selectedStyle
			}
		}
		r := side.New(0, i-a.sidebarTop+2, -1, 1)
		r.Fill(vaxis.Cell{Character: vaxis.Character{Grapheme: " ", Width: 1}, Style: s})
		prefix := "   "
		if a.chats.Items[i].ID == a.opened {
			prefix = " > "
		}
		line(r, 0, prefix+a.chats.Items[i].DisplayName, s, measure)
	}
	content := root.New(sw+2, 0, w-sw-3, h-1)
	if c := a.current(); c != nil {
		headerStyle := vaxis.Style{Attribute: vaxis.AttrBold}
		line(content, 0, c.name, headerStyle, measure)
		cw, ch := content.Size()
		historyHeight := ch - 2
		showDraft := a.mode == insert || len(c.draft.Characters()) > 0
		if showDraft {
			historyHeight -= 4
		}
		c.prepare(c.Items, cw, historyHeight, measure, kittyCell(vx))
		history := content.New(0, 2, cw, historyHeight)
		for i := c.top; i < min(len(c.rows), c.top+historyHeight); i++ {
			r := c.rows[i]
			if r.image != nil {
				if r.imageRow == 0 || i == c.top {
					rows := min(r.image.rows-r.imageRow, c.top+historyHeight-i)
					a.media.draw(vx, history.New(0, i-c.top, cw, rows), r.image, r.imageRow, rows)
				}
			} else {
				line(history, i-c.top, r.text, r.style, measure)
			}
		}
		if len(c.Items) == 0 {
			text := "No messages. Press r to refresh."
			if !c.loaded && c.failure == "" {
				text = "Loading messages..."
			}
			line(content, 2, text, muted, measure)
		}
		info := a.status
		if c.newMessages > 0 {
			info = fmt.Sprintf("%d new messages / G to jump to latest", c.newMessages)
		}
		if c.failure != "" {
			info = c.failure
		}
		line(content, 1, info, muted, measure)
		if showDraft {
			s := muted
			if a.mode == insert {
				s = accent
			}
			line(content, ch-4, "Draft", s, measure)
			line(content, ch-3, "┌"+strings.Repeat("─", cw-2)+"┐", s, measure)
			line(content, ch-2, "│"+strings.Repeat(" ", cw-2)+"│", s, measure)
			line(content, ch-1, "└"+strings.Repeat("─", cw-2)+"┘", s, measure)
			chars := c.draft.Characters()
			for i := range chars {
				chars[i].Width = measure(chars[i].Grapheme)
			}
			c.draft.HideCursor = a.mode != insert
			c.draft.Draw(content.New(1, ch-2, cw-2, 1))
		}
	} else {
		line(content, 0, "Natterwire", accent, measure)
	}
	help := []string{
		" j/k scroll  J/K chats  ^D/^U half-page  G latest  i draft  n more chats  o older  r refresh  q quit",
		" INSERT  Esc sidebar  Ctrl+C quit",
	}[a.mode]
	if a.failure != "" {
		help = a.failure
	}
	line(root, h-1, help, muted, measure)
}
