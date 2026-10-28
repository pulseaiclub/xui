package xui

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/pulseaiclub/xui/cell"
	"github.com/pulseaiclub/xui/render"
	"github.com/pulseaiclub/xui/screen"
)

type syncCapabilityTTY struct {
	*fakeTTY
	output bytes.Buffer
}

func (tty *syncCapabilityTTY) Write(p []byte) (int, error) { return tty.output.Write(p) }

func TestCapabilityReplyRejectsUnknownModesAndMalformedStatus(t *testing.T) {
	for _, raw := range []string{
		"\x1b[?20260;2$y",
		"\x1b[?20270;1$y",
		"\x1b[?2026;0;2$y",
		"\x1b[?2027;0;1$y",
		"\x1b[2026;2$y",
		"\x1b[?2026;2 $y",
		"\x1b[?2026;2y",
		"\x1b[?2026;2$p",
	} {
		t.Run(fmt.Sprintf("%q", raw), func(t *testing.T) {
			vx := &XUI{renderer: render.NewRenderer()}
			loop := NewLoop(vx)
			for i := range len(raw) {
				loop.feed([]byte(raw[i : i+1]))
			}
			if vx.caps.SyncOutput || vx.caps.Unicode {
				t.Fatalf("invalid reply enabled capabilities: %+v", vx.caps)
			}
			if ev, ok := loop.TryEvent(); ok {
				t.Fatalf("invalid reply leaked application event: %#v", ev)
			}
		})
	}
}

func TestSyncCapabilityReplyRender(t *testing.T) {
	for _, status := range []int{0, 1, 2} {
		for _, unicodeStatus := range []int{0, 1, 2} {
			t.Run(fmt.Sprintf("sync=%d/unicode=%d", status, unicodeStatus), func(t *testing.T) {
				tty := &syncCapabilityTTY{fakeTTY: newFakeTTY()}
				vx := &XUI{
					tty:       tty,
					screen:    screen.NewScreen(10, 5),
					renderer:  render.NewRenderer(),
					queryDone: make(chan struct{}),
				}
				// Establish a visible cursor before detecting capabilities.
				vx.screen.SetCursor(6, 3)
				if err := vx.Render(); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(tty.output.String(), render.SeqShowCursor) {
					t.Fatal("initial render did not establish a visible cursor")
				}

				loop := NewLoop(vx)
				raw := fmt.Sprintf("\x1b[?2026;%d$y\x1b[?2027;%d$y\x1b[?62;4c", status, unicodeStatus)
				loop.feed([]byte(raw))
				wantSync, wantUnicode := status != 0, unicodeStatus != 0
				if vx.caps.SyncOutput != wantSync || vx.caps.Unicode != wantUnicode {
					t.Errorf("caps = %+v, want SyncOutput=%t Unicode=%t", vx.caps, wantSync, wantUnicode)
				}
				select {
				case <-vx.queryDone:
				default:
					t.Fatal("DA1 did not complete the capability query")
				}
				if ev, ok := loop.TryEvent(); ok {
					t.Fatalf("capability reply leaked into application events: %#v", ev)
				}

				for _, frame := range []string{"A", "B", "C"} {
					tty.output.Reset()
					vx.screen.SetCell(1, 1, cell.Cell{Char: frame, Width: 1})
					if err := vx.Render(); err != nil {
						t.Fatal(err)
					}
					got := tty.output.String()
					if !strings.Contains(got, frame) {
						t.Fatalf("dirty frame %s was not painted: %q", frame, got)
					}
					if wantSync {
						start := strings.Index(got, render.SeqSyncSet)
						hide := strings.Index(got, render.SeqHideCursor)
						show := strings.LastIndex(got, render.SeqShowCursor)
						if start < 0 || hide <= start || show <= hide || !strings.HasSuffix(got, render.SeqSyncReset) {
							t.Errorf("frame %s did not synchronize painting and cursor visibility: %q", frame, got)
						}
					} else {
						if strings.Contains(got, render.SeqSyncSet) || strings.Contains(got, render.SeqSyncReset) {
							t.Errorf("unsupported sync emitted wrappers: %q", got)
						}
						if !strings.Contains(got, render.SeqHideCursor) || !strings.HasSuffix(got, render.SeqShowCursor) {
							t.Errorf("fallback frame missing hide/show: %q", got)
						}
					}
				}
			})
		}
	}
}
