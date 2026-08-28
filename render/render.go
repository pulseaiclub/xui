package render

import (
	"bytes"
	"fmt"
	"io"
	"strconv"

	"github.com/pulseaiclub/xui/cell"
)

// Renderer converts dirty cells into ANSI escape sequences.
type Renderer struct {
	caps         Caps
	currentStyle cell.Style
	curX, curY   int
	styleValid   bool
	// lastWide tracks the last wide glyph written so a mistaken trail-column
	// dirty cell cannot wipe it (VTE clears the whole wide char).
	lastWideX, lastWideY, lastWideW int
	buf                             bytes.Buffer
}

// Caps holds probed terminal capabilities.
type Caps struct {
	// ColorLevel is the effective color depth (env baseline ± in-band upgrade).
	// Replaces the former RGB bool; RGB ≡ ColorLevel >= cell.ColorTrue.
	ColorLevel cell.ColorLevel

	KittyKeyboard bool
	SyncOutput    bool
	Unicode       bool
	InBandResize  bool

	// KittyGraphics is true when the terminal speaks the kitty graphics
	// protocol (APC ESC _ G ... ESC \) ; Sixel when it reports
	// XTSMGRAPHICS support. Both need a known pixel size to be usable.
	KittyGraphics bool
	Sixel         bool
}

// NewRenderer creates a renderer.
func NewRenderer() *Renderer {
	return &Renderer{}
}

// UpdateCaps refreshes capability-dependent encoding.
func (r *Renderer) UpdateCaps(c Caps) {
	r.caps = c
}

// ResetState clears cursor/style tracking (e.g. after clear screen).
func (r *Renderer) ResetState() {
	r.currentStyle = cell.Style{}
	r.styleValid = false
	r.curX, r.curY = 0, 0
	r.lastWideX, r.lastWideY, r.lastWideW = -1, -1, 0
}

// RenderDiff writes a synchronized frame of dirty cells to w.
func (r *Renderer) RenderDiff(w io.Writer, dirty []cell.DirtyCell, cursorX, cursorY int, cursorVisible bool, cursorShape int) (int, error) {
	r.buf.Reset()
	// Drop any carried SGR / cursor tracking from the previous frame so a
	// missed style clear cannot leave reverse-video ghosts on the tty.
	r.ResetState()
	r.buf.WriteString(seqSGRReset)
	if r.caps.SyncOutput {
		r.buf.WriteString(seqSyncSet)
	}
	r.buf.WriteString(seqHideCursor)

	for _, d := range dirty {
		r.writeCell(&r.buf, d.X, d.Y, d.Cell)
	}

	if cursorVisible {
		r.moveTo(&r.buf, cursorX, cursorY)
		if cursorShape > 0 {
			fmt.Fprintf(&r.buf, csi+"%d q", cursorShape)
		}
		r.buf.WriteString(seqShowCursor)
	} else {
		r.buf.WriteString(seqHideCursor)
	}

	if r.caps.SyncOutput {
		r.buf.WriteString(seqSyncReset)
	}
	n, err := w.Write(r.buf.Bytes())
	return n, err
}

func (r *Renderer) writeCell(buf *bytes.Buffer, x, y int, c cell.Cell) {
	// Wide-glyph trail pads must not be written: a space on the second column
	// of a CJK character erases the glyph in common terminals.
	if c.Trail {
		return
	}
	if r.lastWideW > 1 && y == r.lastWideY && x > r.lastWideX && x < r.lastWideX+r.lastWideW {
		return
	}
	// Reposition if not contiguous.
	if !r.styleValid || r.curX != x || r.curY != y {
		r.moveTo(buf, x, y)
	}
	r.writeStyleDiff(buf, c.Style)
	if !c.Hyperlink.Empty() {
		writeHyperlink(buf, c.Hyperlink)
	}
	ch := c.Char
	if ch == "" {
		ch = " "
	}
	// Never emit raw C0 controls (esp. TAB/CR): the tty interprets them as
	// cursor motion and desyncs Renderer.curX from the real terminal cursor.
	if len(ch) == 1 {
		r0 := ch[0]
		if r0 < 0x20 || r0 == 0x7f {
			ch = " "
		}
	} else {
		for _, rr := range ch {
			if rr < 0x20 || rr == 0x7f {
				ch = " "
				break
			}
		}
	}
	// Wide glyphs painted with Width=1 desync curX (tty advances 2, we track 1)
	// and leave phantom block gaps after CJK — correct before writing.
	width := int(c.Width)
	if width < 1 {
		width = 1
	}
	if disp := cell.StringWidth(ch, cell.WidthUnicode); disp > width {
		width = disp
	}
	buf.WriteString(ch)
	if !c.Hyperlink.Empty() {
		buf.WriteString(osc + "8;;" + st)
	}
	r.curX = x + width
	if r.curX < x+1 {
		r.curX = x + 1
	}
	r.curY = y
	if width > 1 {
		r.lastWideX, r.lastWideY, r.lastWideW = x, y, width
	} else if y != r.lastWideY || x >= r.lastWideX+r.lastWideW {
		r.lastWideX, r.lastWideY, r.lastWideW = -1, -1, 0
	}
}

func (r *Renderer) moveTo(buf *bytes.Buffer, x, y int) {
	buf.WriteString(csi)
	buf.WriteString(strconv.Itoa(y + 1))
	buf.WriteByte(';')
	buf.WriteString(strconv.Itoa(x + 1))
	buf.WriteByte('H')
	r.curX = x
	r.curY = y
}

func (r *Renderer) writeStyleDiff(buf *bytes.Buffer, s cell.Style) {
	// Downgrade before compare so currentStyle tracks what was actually
	// written; distinct RGBs that collapse to one index emit SGR once.
	s.Fg = s.Fg.Downgrade(r.caps.ColorLevel)
	s.Bg = s.Bg.Downgrade(r.caps.ColorLevel)

	if r.styleValid && r.currentStyle.Equal(s) {
		return
	}
	if !r.styleValid {
		buf.WriteString(seqSGRReset)
		r.currentStyle = cell.Style{}
		r.styleValid = true
	}
	cur := &r.currentStyle

	if !cur.Fg.Equal(s.Fg) {
		writeColor(buf, s.Fg, true)
		cur.Fg = s.Fg
	}
	if !cur.Bg.Equal(s.Bg) {
		writeColor(buf, s.Bg, false)
		cur.Bg = s.Bg
	}
	if cur.Bold != s.Bold {
		if s.Bold {
			buf.WriteString(csi + "1m")
		} else {
			buf.WriteString(csi + "22m")
			if s.Dim {
				buf.WriteString(csi + "2m")
			}
		}
		cur.Bold = s.Bold
	}
	if cur.Dim != s.Dim {
		if s.Dim {
			buf.WriteString(csi + "2m")
		} else {
			buf.WriteString(csi + "22m")
			if s.Bold {
				buf.WriteString(csi + "1m")
			}
		}
		cur.Dim = s.Dim
	}
	if cur.Italic != s.Italic {
		if s.Italic {
			buf.WriteString(csi + "3m")
		} else {
			buf.WriteString(csi + "23m")
		}
		cur.Italic = s.Italic
	}
	if cur.Underline != s.Underline {
		if s.Underline {
			buf.WriteString(csi + "4m")
		} else {
			buf.WriteString(csi + "24m")
		}
		cur.Underline = s.Underline
	}
	if cur.Strikethrough != s.Strikethrough {
		if s.Strikethrough {
			buf.WriteString(csi + "9m")
		} else {
			buf.WriteString(csi + "29m")
		}
		cur.Strikethrough = s.Strikethrough
	}
	if cur.Reverse != s.Reverse {
		if s.Reverse {
			buf.WriteString(csi + "7m")
		} else {
			buf.WriteString(csi + "27m")
		}
		cur.Reverse = s.Reverse
	}
}

// writeColor encodes an already-downgraded color as SGR. RGB only appears
// when Caps.ColorLevel is ColorTrue.
func writeColor(buf *bytes.Buffer, c cell.Color, fg bool) {
	switch c.Kind {
	case cell.ColorDefault:
		if fg {
			buf.WriteString(seqFGReset)
		} else {
			buf.WriteString(seqBGReset)
		}
	case cell.ColorIndex:
		switch {
		case c.Index < 8:
			base := 30
			if !fg {
				base = 40
			}
			fmt.Fprintf(buf, csi+"%dm", base+int(c.Index))
		case c.Index < 16:
			base := 90
			if !fg {
				base = 100
			}
			fmt.Fprintf(buf, csi+"%dm", base+int(c.Index)-8)
		default:
			p := "38"
			if !fg {
				p = "48"
			}
			fmt.Fprintf(buf, csi+"%s;5;%dm", p, c.Index)
		}
	case cell.ColorRGB:
		p := "38"
		if !fg {
			p = "48"
		}
		fmt.Fprintf(buf, csi+"%s;2;%d;%d;%dm", p, c.R, c.G, c.B)
	}
}

func writeHyperlink(buf *bytes.Buffer, h cell.Hyperlink) {
	buf.WriteString(osc)
	buf.WriteString("8;")
	if h.ID != "" {
		buf.WriteString("id=")
		buf.WriteString(h.ID)
	}
	buf.WriteByte(';')
	buf.WriteString(h.URI)
	buf.WriteString(st)
}

// EnterAltScreenSeq EnterAltScreen returns the alt-screen enter sequence plus clear.
func EnterAltScreenSeq() string {
	return seqAltEnter + seqSGRReset + seqClearScreen + seqHome + seqHideCursor
}

// ExitAltScreenSeq returns the alt-screen exit sequence.
func ExitAltScreenSeq() string {
	return seqSGRReset + seqShowCursor + seqAltExit
}
