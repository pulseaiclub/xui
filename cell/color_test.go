package cell

import "testing"

func TestRGBToAnsi256(t *testing.T) {
	tests := []struct {
		name    string
		r, g, b uint8
		want    uint8
	}{
		{"black", 0, 0, 0, 16},
		{"white", 255, 255, 255, 231},
		{"mid gray", 128, 128, 128, 243},
		{"red", 255, 0, 0, 196},
		{"green", 0, 255, 0, 46},
		{"blue", 0, 0, 255, 21},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rgbToAnsi256(tt.r, tt.g, tt.b); got != tt.want {
				t.Fatalf("rgbToAnsi256(%d,%d,%d) = %d, want %d", tt.r, tt.g, tt.b, got, tt.want)
			}
		})
	}
}

func TestAnsi256ToAnsiIdx(t *testing.T) {
	tests := []struct {
		name string
		code uint8
		want uint8
	}{
		{"identity 0", 0, 0},
		{"identity 15", 15, 15},
		{"bright red 196", 196, 9},
		{"cube black 16", 16, 0},
		{"cube white 231", 231, 15},
		{"gray low 232", 232, 0},
		{"gray high 255", 255, 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ansi256ToAnsiIdx(tt.code); got != tt.want {
				t.Fatalf("ansi256ToAnsiIdx(%d) = %d, want %d", tt.code, got, tt.want)
			}
		})
	}
}

func TestDowngrade(t *testing.T) {
	red := RGBColor(255, 0, 0)
	idx196 := IndexedColor(196)
	def := DefaultColor()

	tests := []struct {
		name  string
		c     Color
		level ColorLevel
		want  Color
	}{
		{"true keeps rgb", red, ColorTrue, red},
		{"true keeps index", idx196, ColorTrue, idx196},
		{"true keeps default", def, ColorTrue, def},
		{"none → default", red, ColorNone, Color{Kind: ColorDefault}},
		{"none default stays", def, ColorNone, def},
		{"256 rgb → index", red, Color256, IndexedColor(196)},
		{"256 index stays", idx196, Color256, idx196},
		{"basic rgb → 16", red, ColorBasic, IndexedColor(9)},
		{"basic index → 16", idx196, ColorBasic, IndexedColor(9)},
		{"basic low index stays", IndexedColor(3), ColorBasic, IndexedColor(3)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.c.Downgrade(tt.level)
			if !got.Equal(tt.want) {
				t.Fatalf("Downgrade(%v) = %+v, want %+v", tt.level, got, tt.want)
			}
		})
	}
}
