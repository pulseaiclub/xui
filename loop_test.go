package xui

import (
	"io"
	"sync"
	"testing"
	"time"

	"github.com/pulseaiclub/xui/input"
	"github.com/pulseaiclub/xui/term"
)

// fakeTTY feeds scripted bytes to the loop.
//
// It deliberately offers no read deadline: a tty file does not support
// SetReadDeadline, so anything that needs a clock has to bring its own timer.
// Interrupt is a no-op for the same reason (on macOS it silently fails against
// a real tty), and teardown unblocks the reader by closing the input instead.
type fakeTTY struct {
	chunks chan []byte
	closed chan struct{}

	mu      sync.Mutex
	partial []byte
}

func newFakeTTY() *fakeTTY {
	return &fakeTTY{
		chunks: make(chan []byte, 16),
		closed: make(chan struct{}),
	}
}

// Send makes bytes available to the next Read.
func (t *fakeTTY) Send(data string) { t.chunks <- []byte(data) }

// unblock ends the reader, the way closing a terminal does.
func (t *fakeTTY) unblock() {
	t.mu.Lock()
	defer t.mu.Unlock()
	select {
	case <-t.closed:
	default:
		close(t.closed)
	}
}

func (t *fakeTTY) Read(p []byte) (int, error) {
	for {
		t.mu.Lock()
		if len(t.partial) > 0 {
			n := copy(p, t.partial)
			t.partial = t.partial[n:]
			t.mu.Unlock()
			return n, nil
		}
		t.mu.Unlock()

		select {
		case chunk := <-t.chunks:
			t.mu.Lock()
			t.partial = append(t.partial, chunk...)
			t.mu.Unlock()
		case <-t.closed:
			return 0, io.EOF
		}
	}
}

func (t *fakeTTY) Write(p []byte) (int, error)            { return len(p), nil }
func (t *fakeTTY) Size() (cols, rows int, err error)      { return 80, 24, nil }
func (t *fakeTTY) PixelSize() (xpix, ypix int, err error) { return 0, 0, nil }
func (t *fakeTTY) MakeRaw() error                         { return nil }
func (t *fakeTTY) Restore() error                         { return nil }
func (t *fakeTTY) Fd() uintptr                            { return 0 }
func (t *fakeTTY) Close() error                           { return nil }
func (t *fakeTTY) Interrupt()                             {}

var _ term.TTY = (*fakeTTY)(nil)

// newTestLoop starts a loop over a fake terminal. Teardown closes the input
// before stopping the loop, so Stop's Interrupt (a no-op here) is not needed.
func newTestLoop(t *testing.T, escDelay time.Duration) (*Loop, *fakeTTY) {
	t.Helper()
	tty := newFakeTTY()
	loop := NewLoop(&XUI{tty: tty})
	loop.escDelay = escDelay
	loop.Start()
	t.Cleanup(loop.Stop)
	t.Cleanup(tty.unblock) // runs first: LIFO
	return loop, tty
}

// nextKey waits for the next event and requires it to be a key.
func nextKey(t *testing.T, loop *Loop, within time.Duration) input.KeyEvent {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case ev := <-loop.Events():
			if k, ok := ev.(input.KeyEvent); ok {
				return k
			}
		case <-deadline:
			t.Fatalf("no key event within %s", within)
			return input.KeyEvent{}
		}
	}
}

// assertNoKey requires that nothing arrives within d.
func assertNoKey(t *testing.T, loop *Loop, d time.Duration, why string) {
	t.Helper()
	select {
	case ev := <-loop.Events():
		t.Fatalf("%s: got %#v", why, ev)
	case <-time.After(d):
	}
}

// The regression: a bare Esc is ambiguous, so without a timeout it never
// reaches the application at all.
func TestLoopBareEscapeArrives(t *testing.T) {
	loop, tty := newTestLoop(t, 20*time.Millisecond)

	tty.Send("\x1b")
	if k := nextKey(t, loop, time.Second); k.Code != input.KeyEscape {
		t.Fatalf("got %#v", k)
	}
}

// Esc followed promptly by another key is Alt+key: the delay must not split it.
func TestLoopEscapeThenKeyIsAltWithinDelay(t *testing.T) {
	loop, tty := newTestLoop(t, 2*time.Second)

	tty.Send("\x1b")
	tty.Send("j")
	k := nextKey(t, loop, time.Second)
	if k.Rune != 'j' || !k.Mods.Has(input.ModAlt) {
		t.Fatalf("got %#v", k)
	}
	assertNoKey(t, loop, 100*time.Millisecond, "an Alt+key must not also produce an Escape")
}

// Esc, a pause, then a key is two separate keys. Before the timeout the Esc was
// swallowed and the 'j' arrived as Alt+j — the key after Esc stopped working.
func TestLoopEscapeThenKeyAfterDelayIsTwoKeys(t *testing.T) {
	loop, tty := newTestLoop(t, 20*time.Millisecond)

	tty.Send("\x1b")
	if k := nextKey(t, loop, time.Second); k.Code != input.KeyEscape {
		t.Fatalf("got %#v", k)
	}
	tty.Send("j")
	k := nextKey(t, loop, time.Second)
	if k.Rune != 'j' || k.Mods.Has(input.ModAlt) {
		t.Fatalf("got %#v", k)
	}
}

// Two Escapes a delay apart are two Escapes; the second one must not be left
// buffered to swallow the next keypress.
func TestLoopRepeatedEscape(t *testing.T) {
	loop, tty := newTestLoop(t, 20*time.Millisecond)

	for i := range 2 {
		tty.Send("\x1b")
		if k := nextKey(t, loop, time.Second); k.Code != input.KeyEscape {
			t.Fatalf("escape %d: got %#v", i+1, k)
		}
	}
	tty.Send("a")
	if k := nextKey(t, loop, time.Second); k.Rune != 'a' || k.Mods.Has(input.ModAlt) {
		t.Fatalf("got %#v", k)
	}
}

// A sequence split across two reads is one key, even when the gap is longer
// than the Esc timeout would allow a bare Esc.
func TestLoopSplitSequenceStaysOneKey(t *testing.T) {
	loop, tty := newTestLoop(t, 2*time.Second)

	tty.Send("\x1b")
	tty.Send("[A")
	if k := nextKey(t, loop, time.Second); k.Code != input.KeyUp {
		t.Fatalf("got %#v", k)
	}
	assertNoKey(t, loop, 100*time.Millisecond, "an arrow key must not also produce an Escape")
}

// A partial sequence must not be guessed at when the timeout fires: ESC [ is
// not a bare Esc, and it still has to parse as the arrow key it is.
func TestLoopPartialSequenceNotFlushedEarly(t *testing.T) {
	loop, tty := newTestLoop(t, 20*time.Millisecond)

	tty.Send("\x1b[")
	assertNoKey(t, loop, 100*time.Millisecond, "a partial CSI must not become an Escape")

	tty.Send("A")
	if k := nextKey(t, loop, time.Second); k.Code != input.KeyUp {
		t.Fatalf("got %#v", k)
	}
}

// The timeout is off when the delay is negative: Esc stays buffered and merges
// with the next key, which is the pre-fix behavior kept available on purpose.
func TestLoopEscapeTimeoutDisabled(t *testing.T) {
	loop, tty := newTestLoop(t, -1)

	tty.Send("\x1b")
	assertNoKey(t, loop, 200*time.Millisecond, "a disabled timeout must not deliver an Escape")

	tty.Send("j")
	k := nextKey(t, loop, time.Second)
	if k.Rune != 'j' || !k.Mods.Has(input.ModAlt) {
		t.Fatalf("got %#v", k)
	}
}

// A cancelled timer must not leak an Escape into the next keypress.
func TestLoopEscTimerCancelledByNextKey(t *testing.T) {
	loop, tty := newTestLoop(t, 200*time.Millisecond)

	tty.Send("\x1b")
	tty.Send("k") // well inside the delay: Alt+k, not Escape then 'k'
	if k := nextKey(t, loop, time.Second); k.Rune != 'k' || !k.Mods.Has(input.ModAlt) {
		t.Fatalf("got %#v", k)
	}
	assertNoKey(t, loop, 300*time.Millisecond, "the replaced timer must not fire")
}

// Stop must not leave a timer behind that delivers an Escape afterwards.
func TestLoopStopCancelsEscTimer(t *testing.T) {
	tty := newFakeTTY()
	loop := NewLoop(&XUI{tty: tty})
	loop.escDelay = 50 * time.Millisecond
	loop.Start()
	tty.Send("\x1b")
	time.Sleep(10 * time.Millisecond) // the reader has buffered the ESC by now

	// Close the input before Stop: Interrupt cannot unblock a read here (a tty
	// file rejects SetReadDeadline), so Stop would otherwise wait forever.
	tty.unblock()
	loop.Stop()

	loop.parserMu.Lock()
	timer := loop.escTimer
	loop.parserMu.Unlock()
	if timer != nil {
		t.Fatal("Stop must cancel the Esc timer")
	}
}
