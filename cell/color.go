package cell

import "math"

// Color represents a terminal cell color.
type Color struct {
	Kind ColorKind
	// Index is used for KindIndex (0-255).
	Index uint8
	// R, G, B are used for KindRGB.
	R, G, B uint8
}

// ColorKind selects how a Color is encoded.
type ColorKind uint8

const (
	ColorDefault ColorKind = iota
	ColorIndex
	ColorRGB
)

// DefaultColor is the terminal default foreground/background.
func DefaultColor() Color {
	return Color{Kind: ColorDefault}
}

// IndexedColor returns an 8/256-color palette color.
func IndexedColor(index uint8) Color {
	return Color{Kind: ColorIndex, Index: index}
}

// RGBColor returns a truecolor RGB color.
func RGBColor(r, g, b uint8) Color {
	return Color{Kind: ColorRGB, R: r, G: g, B: b}
}

// Equal reports whether two colors are identical.
func (c Color) Equal(o Color) bool {
	if c.Kind != o.Kind {
		return false
	}
	switch c.Kind {
	case ColorDefault:
		return true
	case ColorIndex:
		return c.Index == o.Index
	case ColorRGB:
		return c.R == o.R && c.G == o.G && c.B == o.B
	default:
		return false
	}
}

// Downgrade converts c to the closest expressible color at level.
// It is a pure function safe to call per cell per frame.
func (c Color) Downgrade(level ColorLevel) Color {
	if level >= ColorTrue || c.Kind == ColorDefault {
		return c
	}
	if level == ColorNone {
		// Frame start already emits SGR reset; Default yields monochrome
		// without special-casing the renderer.
		return Color{Kind: ColorDefault}
	}
	switch c.Kind {
	case ColorRGB:
		idx := rgbToAnsi256(c.R, c.G, c.B)
		if level == ColorBasic {
			idx = ansi256ToAnsiIdx(idx)
		}
		return Color{Kind: ColorIndex, Index: idx}
	case ColorIndex:
		if level == ColorBasic {
			return Color{Kind: ColorIndex, Index: ansi256ToAnsiIdx(c.Index)}
		}
		return c
	}
	return c
}

// rgbToAnsi256 maps RGB onto the 256-color palette (color-convert / chalk).
// Grays use the 232–255 ramp; colors snap to the 6×6×6 cube (16–231).
func rgbToAnsi256(r, g, b uint8) uint8 {
	if r == g && g == b {
		if r < 8 {
			return 16
		}
		if r > 248 {
			return 231
		}
		return uint8(int(r-8)*24/247) + 232
	}
	return 16 + 36*round5(r) + 6*round5(g) + round5(b)
}

// round5 maps 0–255 onto a 6×6×6 cube axis 0–5 (rounded).
func round5(v uint8) uint8 {
	return uint8((int(v)*5 + 127) / 255)
}

// ansi256ToAnsiIdx reduces a 256-color index to the nearest 0–15 basic index.
func ansi256ToAnsiIdx(code uint8) uint8 {
	if code < 16 {
		return code
	}
	var r, g, b float64
	if code >= 232 {
		v := (float64(code-232)*10 + 8) / 255
		r, g, b = v, v, v
	} else {
		c := int(code) - 16
		r = float64(c/36) / 5
		g = float64((c%36)/6) / 5
		b = float64(c%6) / 5
	}
	value := math.Max(r, math.Max(g, b)) * 2
	if value == 0 {
		return 0
	}
	idx := uint8(math.Round(b))<<2 | uint8(math.Round(g))<<1 | uint8(math.Round(r))
	if value == 2 {
		idx += 8 // bright variant (SGR 90–97 / 100–107)
	}
	return idx
}
