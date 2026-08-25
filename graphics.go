package xui

import (
	"fmt"
	"io"
)

// placement is a graphics-protocol image drawn outside the cell grid
// (kitty graphics or sixel). Reconcile runs every frame: a placement that
// disappeared since the last frame is deleted, a new one is written, and an
// identical one (same image, size, and origin) is left untouched so the
// terminal keeps rendering the previously uploaded image.
type placement struct {
	id       uint64
	col, row int
	w, h     int
	writeTo  func(w io.Writer)
	deleteFn func(w io.Writer)
}

func samePlacement(p1, p2 *placement) bool {
	return p1.id == p2.id && p1.col == p2.col && p1.row == p2.row && p1.w == p2.w && p1.h == p2.h
}

// reconcilePlacements deletes stale placements and writes new ones. With
// fullRefresh every previous placement is deleted (the screen was cleared)
// and every current one is written. It returns the list to remember as the
// next frame's baseline.
func reconcilePlacements(w io.Writer, last, next []*placement, fullRefresh bool) []*placement {
	if fullRefresh {
		for _, p := range last {
			p.deleteFn(w)
		}
		last = nil
	}
	for _, p1 := range last {
		keep := false
		for _, p2 := range next {
			if samePlacement(p1, p2) {
				keep = true
				break
			}
		}
		if !keep {
			p1.deleteFn(w)
		}
	}
	for _, p1 := range next {
		keep := false
		for _, p2 := range last {
			if samePlacement(p1, p2) {
				keep = true
				break
			}
		}
		if keep {
			continue
		}
		// Graphics protocols place images at the cursor, so move first. The
		// cell renderer repositions itself afterwards, so no desync.
		fmt.Fprintf(w, "\x1b[%d;%dH", p1.row+1, p1.col+1)
		p1.writeTo(w)
	}
	return next
}

func (vx *XUI) nextGraphicID() uint64 {
	vx.graphicsID++
	return vx.graphicsID
}

// addPlacement queues an image placement for the next Render.
func (vx *XUI) addPlacement(p *placement) {
	vx.graphicsNext = append(vx.graphicsNext, p)
}
