package graphics

import (
	"bytes"
	"io"
	"testing"
)

type fakePlacement struct {
	id       uint64
	col, row int
	w, h     int
	writes   int
	deletes  int
}

func (f *fakePlacement) placement() *Placement {
	return &Placement{
		ID:       f.id,
		Col:      f.col,
		Row:      f.row,
		W:        f.w,
		H:        f.h,
		WriteTo:  func(io.Writer) { f.writes++ },
		DeleteFn: func(io.Writer) { f.deletes++ },
	}
}

func TestReconcileWritesNewPlacements(t *testing.T) {
	var buf bytes.Buffer
	f := &fakePlacement{id: 1, col: 2, row: 3, w: 10, h: 5}
	got := ReconcilePlacements(&buf, nil, []*Placement{f.placement()}, false)
	if f.writes != 1 || f.deletes != 0 {
		t.Fatalf("writes=%d deletes=%d, want 1/0", f.writes, f.deletes)
	}
	// Cursor move before the placement write.
	if !bytes.HasPrefix(buf.Bytes(), []byte("\x1b[4;3H")) {
		t.Fatalf("missing cursor move, got %q", buf.Bytes())
	}
	if len(got) != 1 || !samePlacement(got[0], f.placement()) {
		t.Fatalf("baseline not returned: %v", got)
	}
}

func TestReconcileKeepsIdenticalPlacement(t *testing.T) {
	var buf bytes.Buffer
	f := &fakePlacement{id: 7, col: 1, row: 1, w: 4, h: 4}
	last := ReconcilePlacements(&buf, nil, []*Placement{f.placement()}, false)
	buf.Reset()
	next := ReconcilePlacements(&buf, last, []*Placement{f.placement()}, false)
	if f.writes != 1 || f.deletes != 0 {
		t.Fatalf("identical placement re-emitted: writes=%d deletes=%d", f.writes, f.deletes)
	}
	if buf.Len() != 0 {
		t.Fatalf("identical frame wrote %q", buf.Bytes())
	}
	if len(next) != 1 {
		t.Fatalf("next baseline wrong: %v", next)
	}
}

func TestReconcileDeletesStalePlacement(t *testing.T) {
	var buf bytes.Buffer
	f := &fakePlacement{id: 3, col: 0, row: 0, w: 8, h: 8}
	last := ReconcilePlacements(&buf, nil, []*Placement{f.placement()}, false)
	buf.Reset()
	ReconcilePlacements(&buf, last, nil, false)
	if f.deletes != 1 {
		t.Fatalf("stale placement not deleted: %d", f.deletes)
	}
}

func TestReconcileMovedPlacement(t *testing.T) {
	var buf bytes.Buffer
	old := &fakePlacement{id: 5, col: 0, row: 0, w: 8, h: 8}
	last := ReconcilePlacements(&buf, nil, []*Placement{old.placement()}, false)
	buf.Reset()
	moved := &fakePlacement{id: 5, col: 0, row: 3, w: 8, h: 8}
	ReconcilePlacements(&buf, last, []*Placement{moved.placement()}, false)
	if old.deletes != 1 || moved.writes != 1 {
		t.Fatalf("move: deletes=%d writes=%d, want 1/1", old.deletes, moved.writes)
	}
}

func TestReconcileFullRefreshRedrawsAll(t *testing.T) {
	var buf bytes.Buffer
	f := &fakePlacement{id: 9, col: 2, row: 2, w: 6, h: 6}
	last := ReconcilePlacements(&buf, nil, []*Placement{f.placement()}, false)
	buf.Reset()
	// Full refresh: the identical placement is deleted AND rewritten.
	got := ReconcilePlacements(&buf, last, []*Placement{f.placement()}, true)
	if f.writes != 2 || f.deletes != 1 {
		t.Fatalf("full refresh: writes=%d deletes=%d, want 2/1", f.writes, f.deletes)
	}
	if len(got) != 1 {
		t.Fatalf("baseline wrong: %v", got)
	}
}
