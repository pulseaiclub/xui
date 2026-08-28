package render

import (
	"strings"
	"testing"

	"github.com/pulseaiclub/xui/cell"
)

func TestRendererStripsTabControl(t *testing.T) {
	r := NewRenderer()
	var buf mockWriter
	dirty := []cell.DirtyCell{
		{X: 0, Y: 0, Cell: cell.Cell{Char: "A", Width: 1}},
		{X: 1, Y: 0, Cell: cell.Cell{Char: "\t", Width: 1}},
		{X: 2, Y: 0, Cell: cell.Cell{Char: "B", Width: 1}},
	}
	_, err := r.RenderDiff(&buf, dirty, 0, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := string(buf.b)
	if strings.Contains(out, "\t") {
		t.Fatalf("raw tab emitted: %q", out)
	}
	if !strings.Contains(out, "A") || !strings.Contains(out, "B") {
		t.Fatalf("missing glyphs in %q", out)
	}
}

func TestRenderDiffColorLevels(t *testing.T) {
	red := cell.RGBColor(255, 0, 0)
	cellA := cell.Cell{Char: "A", Width: 1, Style: cell.Style{Fg: red}}

	tests := []struct {
		name  string
		level cell.ColorLevel
		want  string // substring that must appear
		deny  string // substring that must not appear
	}{
		{
			name:  "truecolor",
			level: cell.ColorTrue,
			want:  "\x1b[38;2;255;0;0m",
		},
		{
			name:  "256",
			level: cell.Color256,
			want:  "\x1b[38;5;196m",
			deny:  "38;2;",
		},
		{
			name:  "basic",
			level: cell.ColorBasic,
			want:  "\x1b[91m", // bright red compact
			deny:  "38;",
		},
		{
			name:  "none",
			level: cell.ColorNone,
			deny:  "38;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRenderer()
			r.UpdateCaps(Caps{ColorLevel: tt.level})
			var buf mockWriter
			_, err := r.RenderDiff(&buf, []cell.DirtyCell{{X: 0, Y: 0, Cell: cellA}}, 0, 0, false, 0)
			if err != nil {
				t.Fatal(err)
			}
			out := string(buf.b)
			if tt.want != "" && !strings.Contains(out, tt.want) {
				t.Fatalf("missing %q in %q", tt.want, out)
			}
			if tt.deny != "" && strings.Contains(out, tt.deny) {
				t.Fatalf("unexpected %q in %q", tt.deny, out)
			}
			if tt.level == cell.ColorNone {
				if strings.Contains(out, "38;") || strings.Contains(out, "48;") ||
					strings.Contains(out, "\x1b[31m") || strings.Contains(out, "\x1b[91m") {
					t.Fatalf("color SGR in monochrome output: %q", out)
				}
			}
		})
	}
}

func TestRenderDiffCollapsesEqualDowngradedRGB(t *testing.T) {
	// Two distinct RGBs that both map to cube index 196 (bright red).
	a := cell.RGBColor(255, 0, 0)
	b := cell.RGBColor(250, 0, 0) // same cube cell after round5
	r := NewRenderer()
	r.UpdateCaps(Caps{ColorLevel: cell.Color256})
	var buf mockWriter
	dirty := []cell.DirtyCell{
		{X: 0, Y: 0, Cell: cell.Cell{Char: "A", Width: 1, Style: cell.Style{Fg: a}}},
		{X: 1, Y: 0, Cell: cell.Cell{Char: "B", Width: 1, Style: cell.Style{Fg: b}}},
	}
	_, err := r.RenderDiff(&buf, dirty, 0, 0, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := string(buf.b)
	const seq = "\x1b[38;5;196m"
	if n := strings.Count(out, seq); n != 1 {
		t.Fatalf("want 1 SGR for collapsed RGB, got %d in %q", n, out)
	}
}

type mockWriter struct{ b []byte }

func (m *mockWriter) Write(p []byte) (int, error) {
	m.b = append(m.b, p...)
	return len(p), nil
}
