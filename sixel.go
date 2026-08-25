package xui

import (
	"bytes"
	"fmt"
	"image"
)

// encodeSixel encodes img as a sixel device-control string (DCS). Colors are
// quantized to a uniform 6×6×6 RGB cube (216 registers) so no palette table
// has to be computed or shipped; register 0 stays reserved for transparent
// pixels (terminal background).
//
// The band loop mirrors the classic libsixel layout: each band covers six
// pixel rows and is split into per-color passes — a pass writes '#' + color
// select, '$' (carriage return), then run-length-encoded sixel data whose
// bits mark which rows of the band carry that color.
func encodeSixel(img image.Image) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 1 || h < 1 {
		return nil
	}
	var out bytes.Buffer
	out.WriteString("\x1bP0;0;8q")         // DECSIXEL introducer
	fmt.Fprintf(&out, "\"1;1;%d;%d", w, h) // raster attributes: pan=1 pad=1
	// Palette in sixel color space (RGB 0..100).
	for i := 0; i < 216; i++ {
		r, g, bl := i/36, (i/6)%6, i%6
		fmt.Fprintf(&out, "#%d;2;%d;%d;%d", i+1, r*20, g*20, bl*20)
	}

	row := make([]byte, w*217) // bitmask per color register, per column
	bands := (h + 5) / 6
	for z := 0; z < bands; z++ {
		if z > 0 {
			out.WriteByte('-') // DECGNL: next sixel line
		}
		clear(row)
		for p := 0; p < 6; p++ {
			y := z*6 + p
			if y >= h {
				break
			}
			for x := 0; x < w; x++ {
				r16, g16, b16, a16 := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
				if a16 == 0 {
					continue // transparent → background (bit 0)
				}
				// RGBA() is premultiplied; un-premultiply so soft edges
				// (0 < alpha < 255) keep their true color.
				if a16 != 0xFFFF {
					r16 = r16 * 0xFFFF / a16
					g16 = g16 * 0xFFFF / a16
					b16 = b16 * 0xFFFF / a16
				}
				idx := quantize6(r16, g16, b16) + 1
				row[w*idx+x] |= 1 << uint(p)
			}
		}
		first := true
		for n := 1; n <= 216; n++ {
			seg := row[w*n : w*(n+1)]
			if !hasBits(seg) {
				continue
			}
			if !first {
				out.WriteByte('$') // DECGCR: back to column 0 for the next color
			}
			first = false
			fmt.Fprintf(&out, "#%d", n)
			writeSixelRun(&out, seg)
		}
	}
	out.WriteString("\x1b\\") // ST
	return out.Bytes()
}

// quantize6 maps 16-bit RGB channels to the 6×6×6 cube index (0..215).
func quantize6(r16, g16, b16 uint32) int {
	r := int(r16 * 6 / 0x10000)
	g := int(g16 * 6 / 0x10000)
	bl := int(b16 * 6 / 0x10000)
	if r > 5 {
		r = 5
	}
	if g > 5 {
		g = 5
	}
	if bl > 5 {
		bl = 5
	}
	return r*36 + g*6 + bl
}

func hasBits(seg []byte) bool {
	for _, v := range seg {
		if v != 0 {
			return true
		}
	}
	return false
}

// writeSixelRun writes run-length-encoded sixel data ('!N' + char for runs
// longer than 3, else the repeated chars). Trailing zero columns are dropped:
// they only advance the drawing position and have no visible effect.
func writeSixelRun(out *bytes.Buffer, row []byte) {
	for len(row) > 0 && row[len(row)-1] == 0 {
		row = row[:len(row)-1]
	}
	last := byte(0)
	count := 0
	flush := func() {
		if count == 0 {
			return
		}
		ch := last + '?'
		switch {
		case count <= 3:
			for i := 0; i < count; i++ {
				out.WriteByte(ch)
			}
		default:
			fmt.Fprintf(out, "!%d%c", count, ch)
		}
		count = 0
	}
	for _, ch := range row {
		if count != 0 && ch != last {
			flush()
		}
		last = ch
		count++
	}
	flush()
}
