package xui

import (
	"image"

	"github.com/pulseaiclub/xui/graphics"
)

// NewImage creates an image using the highest quality renderer the terminal
// supports: kitty graphics, sixel, or half-block cells. The half-block
// fallback works everywhere; the pixel-addressed protocols require a
// reported pixel size.
func (vx *XUI) NewImage(img image.Image) (graphics.Image, error) {
	caps := vx.Caps()
	w, h := vx.cellPixelSize()
	switch {
	case caps.KittyGraphics && w > 0 && h > 0:
		return graphics.NewKittyImage(vx, img), nil
	case caps.Sixel && w > 0 && h > 0:
		return graphics.NewSixel(vx, img), nil
	default:
		return graphics.NewHalfBlockImage(img), nil
	}
}

// NewKittyGraphic forces the kitty graphics protocol renderer.
func (vx *XUI) NewKittyGraphic(img image.Image) *graphics.KittyImage {
	return graphics.NewKittyImage(vx, img)
}

// NewSixel forces the sixel renderer.
func (vx *XUI) NewSixel(img image.Image) *graphics.Sixel {
	return graphics.NewSixel(vx, img)
}

// NewHalfBlockImage forces the half-block cell renderer.
func (vx *XUI) NewHalfBlockImage(img image.Image) *graphics.HalfBlockImage {
	return graphics.NewHalfBlockImage(img)
}

// RemoveImage drops all queued placements for img so it vanishes on the next
// Render without another Draw call (the image stays uploaded until Destroy).
func (vx *XUI) RemoveImage(img graphics.Image) {
	id, ok := graphics.ID(img)
	if !ok {
		return
	}
	next := vx.graphicsNext[:0]
	for _, p := range vx.graphicsNext {
		if p.ID != id {
			next = append(next, p)
		}
	}
	vx.graphicsNext = next
}

// NextGraphicID hands out image ids. Locked: NewImage may be called from a
// non-UI goroutine (e.g. after an async decode), and a duplicate kitty id
// would silently replace a previously uploaded image in the terminal store.
func (vx *XUI) NextGraphicID() uint64 {
	vx.mu.Lock()
	defer vx.mu.Unlock()
	vx.graphicsID++
	return vx.graphicsID
}

// AddPlacement queues an image placement for the next Render.
func (vx *XUI) AddPlacement(p *graphics.Placement) {
	vx.graphicsNext = append(vx.graphicsNext, p)
}

// WriteControl writes a control sequence straight to the TTY.
func (vx *XUI) WriteControl(s string) {
	vx.mu.Lock()
	defer vx.mu.Unlock()
	_, _ = vx.tty.Write([]byte(s))
}
