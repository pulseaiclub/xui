package xui

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/pulseaiclub/xui/cell"
)

func TestKittyUploadChunksSingle(t *testing.T) {
	b64 := strings.Repeat("A", 100)
	out := kittyUploadChunks(42, b64)
	s := string(out)
	if !strings.HasPrefix(s, "\x1B_Gf=100,i=42,m=0;") || !strings.HasSuffix(s, "\x1b\\") {
		t.Fatalf("bad single chunk: %q", s)
	}
	if strings.Count(s, "\x1b\\") != 1 {
		t.Fatalf("expected 1 chunk, got %d", strings.Count(s, "\x1b\\"))
	}
}

func TestKittyUploadChunksSplit(t *testing.T) {
	// 3 payloads of 4096: two m=1 chunks and one final m=0 chunk.
	b64 := strings.Repeat("B", 4096*3)
	out := kittyUploadChunks(7, b64)
	s := string(out)
	if strings.Count(s, "m=1;") != 2 || !strings.Contains(s, "m=0;") || strings.Count(s, "\x1b\\") != 3 {
		t.Fatalf("bad chunk split: %d m=1 chunks", strings.Count(s, "m=1;"))
	}
}

func TestHalfBlockTwoTone(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	img.Set(0, 1, color.RGBA{R: 0, G: 0, B: 255, A: 255})
	hb := &HalfBlockImage{img: img}
	hb.Resize(10, 10)
	w, h := hb.CellSize()
	if w != 2 || h != 1 {
		t.Fatalf("cell size %dx%d, want 2x1", w, h)
	}
	c := hb.cells[0]
	if c.Char != "▀" {
		t.Fatalf("char %q, want upper half block", c.Char)
	}
	r, g, b := c.Style.Fg.R, c.Style.Fg.G, c.Style.Fg.B
	if !(r > 200 && g < 50 && b < 50) {
		t.Fatalf("fg not red: %+v", c.Style.Fg)
	}
	r, g, b = c.Style.Bg.R, c.Style.Bg.G, c.Style.Bg.B
	if !(b > 200 && r < 50) {
		t.Fatalf("bg not blue: %+v", c.Style.Bg)
	}
}

func TestHalfBlockTransparentTop(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 2))
	img.Set(0, 0, color.RGBA{R: 0, G: 0, B: 0, A: 0})
	img.Set(0, 1, color.RGBA{R: 0, G: 255, B: 0, A: 255})
	hb := &HalfBlockImage{img: img}
	hb.Resize(5, 5)
	c := hb.cells[0]
	if c.Char != "▄" {
		t.Fatalf("char %q, want lower half block", c.Char)
	}
	if c.Style.Bg != (cell.Color{}) {
		t.Fatalf("bg should stay default: %+v", c.Style.Bg)
	}
}

func TestResizeImageNeverUpscales(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	got := resizeImage(img, 100, 100, 8, 16)
	if got != image.Image(img) {
		t.Fatalf("small image should be returned unchanged")
	}
}

func TestResizeImageFitsArea(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 50))
	got := resizeImage(img, 10, 10, 8, 16)
	b := got.Bounds()
	// 100x50 px → 13x4 cells; fit into 10x10 cells keeps aspect ratio.
	if b.Dx() > 100 || b.Dy() > 50 {
		t.Fatalf("upscaled: %dx%d", b.Dx(), b.Dy())
	}
	// Aspect ratio must be preserved (100:50 = 2:1).
	if b.Dx() != 2*b.Dy() {
		t.Fatalf("aspect broken: %dx%d", b.Dx(), b.Dy())
	}
}

func TestQuantize6(t *testing.T) {
	if q := quantize6(65535, 0, 0); q != 180 {
		t.Fatalf("red idx=%d, want 180", q)
	}
	if q := quantize6(0, 65535, 0); q != 30 {
		t.Fatalf("green idx=%d, want 30", q)
	}
	if q := quantize6(0, 0, 65535); q != 5 {
		t.Fatalf("blue idx=%d, want 5", q)
	}
	if q := quantize6(0, 0, 0); q != 0 {
		t.Fatalf("black idx=%d, want 0", q)
	}
}

func TestEncodeSixelSolidColor(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 2; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	out := encodeSixel(img)
	s := string(out)
	if !strings.HasPrefix(s, "\x1bP0;0;8q") {
		t.Fatalf("missing DCS introducer: %q", s)
	}
	if !strings.HasPrefix(s[8:], "\"1;1;2;3") {
		t.Fatalf("missing raster attributes: %q", s)
	}
	if !strings.HasSuffix(s, "\x1b\\") {
		t.Fatalf("missing ST: %q", s)
	}
	// Red → cube index 180 → register 181.
	if !strings.Contains(s, "#181") {
		t.Fatalf("missing red register: %q", s)
	}
	// 3 rows → bits 0,1,2 set → char '?'+0b111 = 'F', twice (2 columns).
	if !strings.Contains(s, "FF") {
		t.Fatalf("missing sixel data: %q", s)
	}
}

func TestEncodeSixelBands(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1, 7))
	for y := 0; y < 7; y++ {
		img.Set(0, y, color.RGBA{R: 0, G: 255, B: 0, A: 255})
	}
	out := string(encodeSixel(img))
	// 7 rows → two bands separated by DECGNL '-'.
	if strings.Count(out, "-") != 1 {
		t.Fatalf("expected 1 band separator, got %q", out)
	}
}

func TestEncodeSixelTransparent(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	out := encodeSixel(img)
	if out == nil {
		t.Fatal("nil output for transparent image")
	}
	// An invisible image emits no band data: the stream ends right after the
	// last palette entry, with no sixel characters in between.
	if !strings.HasSuffix(string(out), ";100;100;100\x1b\\") {
		t.Fatalf("band data emitted for empty image: %q", out)
	}
}
