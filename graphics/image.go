package graphics

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"

	"github.com/pulseaiclub/xui/cell"
	"github.com/pulseaiclub/xui/screen"
)

// Alpha value that counts as transparent enough to fall back to the terminal
// background (matches libvaxis).
const transparentEnough = 50

// Image is a static image on the screen. Create one with the engine's
// NewImage (or the package constructors), size it with Resize to fit the
// target cell area, and call Draw from the widget's render pass every frame.
// Call Destroy when done.
type Image interface {
	// Draw places the image at the window origin. Must be called every frame
	// the image is visible; identical placements are skipped by the engine.
	Draw(win screen.Window)
	// Destroy removes the image from terminal memory.
	Destroy()
	// Resize scales the image to fit the w×h cell area without upscaling.
	Resize(w, h int)
	// CellSize returns the current image size in cells.
	CellSize() (w, h int)
}

// NewKittyImage renders via the kitty graphics protocol (APC).
func NewKittyImage(e Engine, img image.Image) *KittyImage {
	return &KittyImage{e: e, img: img, id: e.NextGraphicID()}
}

// NewSixel renders via the sixel graphics protocol (DCS).
func NewSixel(e Engine, img image.Image) *Sixel {
	return &Sixel{e: e, img: img, id: e.NextGraphicID()}
}

// NewHalfBlockImage renders with half-block cells and needs no engine.
func NewHalfBlockImage(img image.Image) *HalfBlockImage {
	return &HalfBlockImage{img: img}
}

// ID returns the protocol id of img and whether it has one. Kitty and sixel
// images are id'd (the terminal stores the bitmap under this id); half-block
// images are plain cells and have no id.
func ID(img Image) (uint64, bool) {
	switch img := img.(type) {
	case *KittyImage:
		return img.id, true
	case *Sixel:
		return img.id, true
	default:
		return 0, false
	}
}

// KittyImage renders via the kitty graphics protocol (APC). The PNG is
// encoded synchronously on Resize; uploading is deferred until the first
// Draw so an image that never becomes visible costs nothing.
type KittyImage struct {
	e        Engine
	img      image.Image
	id       uint64
	w, h     int // cell size
	uploaded bool
	buf      []byte // APC upload chunks, built by Resize
}

// Draw places the image at the window origin.
func (k *KittyImage) Draw(win screen.Window) {
	if k.w == 0 || k.h == 0 {
		return
	}
	// Skip images that do not fit the window: the placement CUP would clamp
	// past the screen edge and the bitmap would land on unrelated content.
	if w, h := win.Size(); k.w > w || k.h > h {
		return
	}
	col, row := win.Origin()
	pid := uint(col)<<16 | uint(row)
	k.e.AddPlacement(&Placement{
		ID:  k.id,
		Col: col,
		Row: row,
		W:   k.w,
		H:   k.h,
		WriteTo: func(w io.Writer) {
			if !k.uploaded {
				_, _ = w.Write(k.buf)
				k.uploaded = true
			}
			// Re-create the placement at the current cursor (the engine moved
			// it to the origin). Same id + pid: the terminal keeps the image.
			_, _ = fmt.Fprintf(w, "\x1B_Ga=p,i=%d,p=%d,C=1\x1B\\", k.id, pid)
		},
		DeleteFn: func(w io.Writer) {
			_, _ = fmt.Fprintf(w, "\x1B_Ga=d,d=i,i=%d,p=%d\x1B\\", k.id, pid)
		},
	})
}

// Destroy removes the image from terminal memory. The uploaded flag is
// cleared so a stray Draw afterwards re-uploads instead of placing a dead id.
func (k *KittyImage) Destroy() {
	k.e.WriteControl(fmt.Sprintf("\x1B_Ga=d,d=I,i=%d\x1B\\", k.id))
	k.uploaded = false
}

func (k *KittyImage) CellSize() (w, h int) { return k.w, k.h }

// Resize scales and re-encodes the image to fit the w×h cell area.
func (k *KittyImage) Resize(w, h int) {
	cellPixW, cellPixH := k.e.CellPixelSize()
	if cellPixW <= 0 || cellPixH <= 0 {
		return
	}
	img := resizeImage(k.img, w, h, cellPixW, cellPixH)
	max := img.Bounds().Max
	k.w = (max.X + cellPixW - 1) / cellPixW
	k.h = (max.Y + cellPixH - 1) / cellPixH
	k.uploaded = false

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		return
	}
	b64 := base64.StdEncoding.EncodeToString(pngBuf.Bytes())
	k.buf = kittyUploadChunks(k.id, b64)
}

// Sixel renders via the sixel graphics protocol (DCS). The bitmap is encoded
// synchronously on Resize. Its cells are reserved on the screen so the diff
// engine never paints text over the image.
type Sixel struct {
	e        Engine
	img      image.Image
	id       uint64
	w, h     int // cell size
	buf      []byte
	cellPixW int
	cellPixH int
}

// Draw places the image at the window origin and reserves its cells.
func (s *Sixel) Draw(win screen.Window) {
	if s.w == 0 || s.h == 0 || s.buf == nil {
		return
	}
	// Skip images that do not fit the window: sixel output at the screen edge
	// scrolls or clips, and the delete pass would clamp onto unrelated rows.
	if w, h := win.Size(); s.w > w || s.h > h {
		return
	}
	for y := 0; y < s.h; y++ {
		for x := 0; x < s.w; x++ {
			win.SetCell(x, y, cell.Cell{Sixel: true})
		}
	}
	col, row := win.Origin()
	s.e.AddPlacement(&Placement{
		ID:  s.id,
		Col: col,
		Row: row,
		W:   s.w,
		H:   s.h,
		WriteTo: func(w io.Writer) {
			_, _ = w.Write(s.buf)
		},
		DeleteFn: func(w io.Writer) {
			// SGR reset first: ECH erases with the current background, and the
			// frame's last cell style is still active here (placements run
			// before the cell diff). ECH (\x1b[X) clears both the text and the
			// sixel graphics plane — plain spaces leave the bitmap in place on
			// xterm-class terminals.
			_, _ = io.WriteString(w, "\x1b[m")
			for y := 0; y < s.h; y++ {
				_, _ = fmt.Fprintf(w, "\x1b[%d;%dH\x1b[%dX", row+y+1, col+1, s.w)
			}
		},
	})
}

// Destroy frees the encoded bitmap.
func (s *Sixel) Destroy() { s.buf = nil }

func (s *Sixel) CellSize() (w, h int) { return s.w, s.h }

// Resize scales and re-encodes the image to fit the w×h cell area.
func (s *Sixel) Resize(w, h int) {
	cellPixW, cellPixH := s.e.CellPixelSize()
	if cellPixW <= 0 || cellPixH <= 0 {
		return
	}
	s.cellPixW, s.cellPixH = cellPixW, cellPixH
	img := resizeImage(s.img, w, h, cellPixW, cellPixH)
	max := img.Bounds().Max
	s.w = (max.X + cellPixW - 1) / cellPixW
	s.h = (max.Y + cellPixH - 1) / cellPixH
	s.buf = encodeSixel(img)
}

// HalfBlockImage renders with half-block characters (▀/▄). It is plain cells,
// so it needs no placement and works on any terminal.
type HalfBlockImage struct {
	img           image.Image
	cells         []cell.Cell
	width, height int
}

// Draw writes the image cells at the window origin.
func (hb *HalfBlockImage) Draw(win screen.Window) {
	for i, c := range hb.cells {
		y := i / hb.width
		x := i - y*hb.width
		win.SetCell(x, y, c)
	}
}

func (hb *HalfBlockImage) Destroy() { hb.cells = nil }

func (hb *HalfBlockImage) CellSize() (w, h int) { return hb.width, hb.height }

// Resize scales the image to fit the w×h cell area with a 1×2 pixel-per-cell
// geometry (the terminal cell is one column wide and two rows tall).
func (hb *HalfBlockImage) Resize(w, h int) {
	img := resizeImage(hb.img, w, h, 1, 2)
	max := img.Bounds().Max
	hb.width = max.X
	hh := max.Y
	if hh%2 != 0 {
		hh++
	}
	hb.height = hh / 2
	hb.cells = make([]cell.Cell, hb.width*hb.height)
	for i := range hb.cells {
		y := i / hb.width
		x := i - y*hb.width
		y *= 2
		tr, tg, tb, ta := toRGB(img.At(x, y))
		br, bg, bb, ba := toRGB(img.At(x, y+1))
		switch {
		case ta < transparentEnough && ba < transparentEnough:
			hb.cells[i] = cell.Cell{Char: " ", Width: 1}
		case ta < transparentEnough:
			hb.cells[i] = cell.Cell{Char: "▄", Width: 1, Style: cell.Style{Fg: cell.RGBColor(br, bg, bb)}}
		default:
			// Upper half block with both halves colored: the top pixel is the
			// foreground, the bottom the background.
			st := cell.Style{Fg: cell.RGBColor(tr, tg, tb)}
			if ba >= transparentEnough {
				st.Bg = cell.RGBColor(br, bg, bb)
			}
			hb.cells[i] = cell.Cell{Char: "▀", Width: 1, Style: st}
		}
	}
}

// kittyUploadChunks splits base64-encoded PNG data into APC upload chunks of
// at most 4096 bytes; the last chunk carries m=0 to signal completion.
func kittyUploadChunks(id uint64, b64 string) []byte {
	var b strings.Builder
	for len(b64) > 4096 {
		fmt.Fprintf(&b, "\x1B_Gf=100,i=%d,m=1;%s\x1B\\", id, b64[:4096])
		b64 = b64[4096:]
	}
	fmt.Fprintf(&b, "\x1B_Gf=100,i=%d,m=0;%s\x1B\\", id, b64)
	return []byte(b.String())
}

// resizeImage scales img down to fit the w×h cell area (cellPixW×cellPixH
// pixels per cell), preserving aspect ratio and never upscaling.
func resizeImage(img image.Image, w, h, cellPixW, cellPixH int) image.Image {
	// Dx/Dy, not Bounds().Max: cropped sub-images have a non-zero Min origin.
	wPix := img.Bounds().Dx()
	hPix := img.Bounds().Dy()
	columns := (wPix + cellPixW - 1) / cellPixW
	lines := (hPix + cellPixH - 1) / cellPixH
	if columns <= w && lines <= h {
		return img
	}
	// Scale by the smaller factor so the image fits fully.
	sf := float64(w) / float64(columns)
	if sfy := float64(h) / float64(lines); sfy < sf {
		sf = sfy
	}
	nw := int(sf * float64(wPix))
	nh := int(sf * float64(hPix))
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	scaleNearest(dst, img)
	return dst
}

func scaleNearest(dst *image.RGBA, src image.Image) {
	db := dst.Bounds()
	sb := src.Bounds()
	dx, dy := db.Dx(), db.Dy()
	sx, sy := sb.Dx(), sb.Dy()
	for y := 0; y < dy; y++ {
		srcY := sb.Min.Y + y*sy/dy
		for x := 0; x < dx; x++ {
			dst.Set(x+db.Min.X, y+db.Min.Y, src.At(sb.Min.X+x*sx/dx, srcY))
		}
	}
}

func toRGB(c color.Color) (uint8, uint8, uint8, uint8) {
	pr, pg, pb, pa := c.RGBA()
	var r, g, b, a uint8
	switch pa {
	case 0:
		r, g, b = uint8(pr), uint8(pg), uint8(pb)
	default:
		r = uint8((pr * 255) / pa)
		g = uint8((pg * 255) / pa)
		b = uint8((pb * 255) / pa)
		a = uint8(pa >> 8)
	}
	return r, g, b, a
}
