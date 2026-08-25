// Package graphics implements terminal image protocols — kitty graphics
// (APC), sixel (DCS), and half-block cells — plus the placement diff that
// keeps protocol images on screen across frames. The engine's XUI implements
// [Engine]; applications hold [Image] values and draw them into a window.
package graphics

import (
	"fmt"
	"io"
)

// Engine is the subset of the xui engine the protocol implementations need:
// the cell pixel size for sizing, protocol ids for terminal-side image
// handles, placement queueing, and direct control writes for teardown.
// *xui.XUI implements it; keeping it an interface lets this package stay
// independent of the engine.
type Engine interface {
	CellPixelSize() (w, h int)
	NextGraphicID() uint64
	AddPlacement(p *Placement)
	WriteControl(s string)
}

// Placement is a graphics-protocol image drawn outside the cell grid (kitty
// graphics or sixel). ReconcilePlacements runs every frame: a placement that
// disappeared since the last frame is deleted, a new one is written, and an
// identical one (same image, size, and origin) is left untouched so the
// terminal keeps rendering the previously uploaded image.
type Placement struct {
	ID       uint64
	Col, Row int
	W, H     int
	WriteTo  func(w io.Writer)
	DeleteFn func(w io.Writer)
}

func samePlacement(p1, p2 *Placement) bool {
	return p1.ID == p2.ID && p1.Col == p2.Col && p1.Row == p2.Row && p1.W == p2.W && p1.H == p2.H
}

// ReconcilePlacements deletes stale placements and writes new ones. With
// fullRefresh every previous placement is deleted (the screen was cleared)
// and every current one is written. It returns the list to remember as the
// next frame's baseline.
func ReconcilePlacements(w io.Writer, last, next []*Placement, fullRefresh bool) []*Placement {
	if fullRefresh {
		for _, p := range last {
			p.DeleteFn(w)
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
			p1.DeleteFn(w)
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
		fmt.Fprintf(w, "\x1b[%d;%dH", p1.Row+1, p1.Col+1)
		p1.WriteTo(w)
	}
	return next
}
