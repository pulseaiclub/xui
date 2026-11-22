package screen

import (
	"testing"

	"github.com/pulseaiclub/xui/cell"
)

// An overlay (listpicker border) painting into the continuation column of a
// wide glyph must truncate that glyph: the primary still claims Width=2, so
// width-stepping in Diff/emitRow skips the overwritten column forever and the
// border never reaches the tty — the CJK glyph keeps covering it.
func TestSetCellIntoWideContinuationTruncatesPrimary(t *testing.T) {
	s := NewScreen(10, 1)

	// Frame 1: background transcript paints a CJK glyph at columns 3..4.
	s.SetCell(3, 0, cell.Cell{Char: "文", Width: 2})
	s.MarkRefresh()
	s.Diff()
	s.Present()

	// Frame 2: the same background is repainted, then an overlay paints its
	// left border into the glyph's continuation column.
	s.SetCell(3, 0, cell.Cell{Char: "文", Width: 2})
	s.SetCell(4, 0, cell.Cell{Char: "│", Width: 1})

	if got := s.GetCell(3, 0); got.Width != 1 {
		t.Fatalf("primary not truncated after trail overwrite: %+v", got)
	}
	dirty := map[int]cell.Cell{}
	for _, d := range s.Diff() {
		dirty[d.X] = d.Cell
	}
	b, ok := dirty[4]
	if !ok {
		t.Fatal("border cell at x=4 never emitted")
	}
	if b.Char != "│" {
		t.Fatalf("x=4 = %+v", b)
	}
	s.Present()
}
