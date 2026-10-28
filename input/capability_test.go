package input

import (
	"fmt"
	"reflect"
	"testing"
)

func TestParserDECRPM(t *testing.T) {
	for _, mode := range []int{2026, 2027} {
		for _, status := range []int{0, 1, 2} {
			raw := fmt.Sprintf("\x1b[?%d;%d$y", mode, status)
			want := []Event{CapEvent{Kind: CapDECRQM, Data: raw}}
			// Include unsplit input and every possible two-chunk boundary.
			for split := 0; split <= len(raw); split++ {
				t.Run(fmt.Sprintf("mode=%d/status=%d/split=%d", mode, status, split), func(t *testing.T) {
					p := NewParser()
					got := p.Feed([]byte(raw[:split]))
					if split < len(raw) && len(got) != 0 {
						t.Fatalf("incomplete reply emitted events: %#v", got)
					}
					got = append(got, p.Feed([]byte(raw[split:]))...)
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("events = %#v, want %#v", got, want)
					}
				})
			}
		}
	}
}

func TestParserDECRPMRejectsUnrelatedCSI(t *testing.T) {
	for _, raw := range []string{
		"\x1b[y",
		"\x1b[?2026;2y",  // Missing DECRPM intermediate.
		"\x1b[?2026;2 y", // Different intermediate.
		"\x1b[?2026;2$ y",
		"\x1b[2026;2y",
		"\x1b[p",
		"\x1b[?2026$p", // DECRQM request, not a reply.
		"\x1b[?2027$p",
		"\x1b[?2026;2$p",
	} {
		t.Run(fmt.Sprintf("%q", raw), func(t *testing.T) {
			p := NewParser()
			if got := p.Feed([]byte(raw)); len(got) != 0 {
				t.Fatalf("unrelated CSI emitted events: %#v", got)
			}
			// Ignoring a sequence must still consume it completely.
			want := []Event{KeyEvent{Code: KeyRune, Rune: 'a', Text: "a", Press: true}}
			if got := p.Feed([]byte("a")); !reflect.DeepEqual(got, want) {
				t.Fatalf("following key = %#v, want %#v", got, want)
			}
		})
	}
}

func TestParserDECRPMConcatenated(t *testing.T) {
	const syncReply = "\x1b[?2026;2$y"
	const unicodeReply = "\x1b[?2027;1$y"
	const da1 = "\x1b[?62;4c"
	raw := syncReply + unicodeReply + da1
	want := []Event{
		CapEvent{Kind: CapDECRQM, Data: syncReply},
		CapEvent{Kind: CapDECRQM, Data: unicodeReply},
		CapEvent{Kind: CapDA1, Data: da1},
	}
	for chunkSize := 1; chunkSize <= len(raw); chunkSize++ {
		t.Run(fmt.Sprintf("chunk=%d", chunkSize), func(t *testing.T) {
			p := NewParser()
			var got []Event
			for start := 0; start < len(raw); start += chunkSize {
				got = append(got, p.Feed([]byte(raw[start:min(start+chunkSize, len(raw))]))...)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("events = %#v, want %#v", got, want)
			}
		})
	}
}
