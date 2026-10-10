package xui

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pulseaiclub/xui/input"
)

// DefaultEscDelay is how long a lone ESC waits for the byte that would make it
// an escape sequence before it is delivered as the Escape key.
//
// Esc, arrows, function keys, Alt+key and mouse reports are all byte sequences
// beginning with ESC, so one byte on its own is ambiguous. The delay is what
// separates a bare Esc from the rest; a sequence arriving in time cancels it,
// so the only cost is that Esc lands this much later than the keypress.
const DefaultEscDelay = 50 * time.Millisecond

// Loop reads TTY input on a background goroutine and delivers Events.
type Loop struct {
	vx      *XUI
	parser  *input.Parser
	events  chan input.Event
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running atomic.Bool

	// escDelay is Options.EscDelay, or DefaultEscDelay when that is zero.
	// Negative disables the timeout, leaving a bare Esc buffered until the next
	// key arrives and merges with it as Alt+key.
	escDelay time.Duration

	// parserMu keeps the reader and the Esc timer off each other's bytes: both
	// Feed (reader) and FlushEsc (timer) mutate the same buffer.
	parserMu sync.Mutex
	escTimer *time.Timer
	// escGen invalidates a timer that has already been superseded, so a stale
	// one cannot flush the ESC of a later keypress early.
	escGen uint64
}

// NewLoop creates an event loop bound to vx.
func NewLoop(vx *XUI) *Loop {
	loop := &Loop{
		vx:       vx,
		parser:   input.NewParser(),
		events:   make(chan input.Event, 512),
		escDelay: DefaultEscDelay,
	}
	if vx != nil && vx.opts.EscDelay != 0 {
		loop.escDelay = vx.opts.EscDelay
	}
	return loop
}

// Start begins the reader goroutine.
func (l *Loop) Start() {
	if l.running.Swap(true) {
		return
	}
	if clearer, ok := l.vx.tty.(interface{ ClearDeadline() }); ok {
		clearer.ClearDeadline()
	}
	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.wg.Add(1)
	go l.readLoop(ctx)
}

// Stop stops the reader goroutine.
func (l *Loop) Stop() {
	if !l.running.Swap(false) {
		return
	}
	l.stopEscTimer()
	if l.cancel != nil {
		l.cancel()
	}
	if l.vx != nil && l.vx.tty != nil {
		l.vx.tty.Interrupt()
	}
	l.wg.Wait()
}

// NextEvent blocks until an application event is available.
func (l *Loop) NextEvent() input.Event { return <-l.events }

// TryEvent returns an event if one is pending.
func (l *Loop) TryEvent() (input.Event, bool) {
	select {
	case e := <-l.events:
		return e, true
	default:
		return nil, false
	}
}

// Events exposes the event channel.
func (l *Loop) Events() <-chan input.Event { return l.events }

// Post injects an event (e.g. Resize from SIGWINCH).
func (l *Loop) Post(ev input.Event) {
	select {
	case l.events <- ev:
	default:
	}
}

func (l *Loop) readLoop(ctx context.Context) {
	defer l.wg.Done()
	buf := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := l.vx.tty.Read(buf)
		if n > 0 {
			l.feed(buf[:n])
		}
		if err != nil {
			if err == io.EOF {
				return
			}
			select {
			case <-ctx.Done():
				return
			default:
				continue
			}
		}
	}
}

// feed parses bytes from the terminal and re-arms the Esc timeout. A read that
// leaves a lone ESC buffered is the only case that needs a timer; every other
// state resolves the byte itself.
func (l *Loop) feed(data []byte) {
	l.parserMu.Lock()
	events := l.parser.Feed(data)
	pending := l.parser.PendingEsc()
	l.parserMu.Unlock()
	for _, ev := range events {
		l.handle(ev)
	}
	l.armEscTimer(pending)
}

// armEscTimer schedules the timeout that resolves a lone ESC into the Escape
// key. It runs on its own goroutine rather than through a read deadline: a tty
// file does not support SetReadDeadline ("file type does not support
// deadline"), so the timer cannot borrow the reader's clock. Flushing is still
// safe from here because PendingEsc re-checks the parser at fire time.
func (l *Loop) armEscTimer(pending bool) {
	l.parserMu.Lock()
	defer l.parserMu.Unlock()
	if l.escTimer != nil {
		l.escTimer.Stop()
		l.escTimer = nil
	}
	if !pending || l.escDelay <= 0 {
		return
	}
	l.escGen++
	gen := l.escGen
	l.escTimer = time.AfterFunc(l.escDelay, func() { l.flushEsc(gen) })
}

// flushEsc delivers a pending lone ESC as the Escape key, unless a later
// keypress already replaced it (gen mismatch) or there is nothing to resolve.
func (l *Loop) flushEsc(gen uint64) {
	l.parserMu.Lock()
	if gen != l.escGen {
		l.parserMu.Unlock()
		return
	}
	l.escTimer = nil
	events := l.parser.FlushEsc()
	l.parserMu.Unlock()
	for _, ev := range events {
		l.handle(ev)
	}
}

func (l *Loop) stopEscTimer() {
	l.parserMu.Lock()
	defer l.parserMu.Unlock()
	if l.escTimer != nil {
		l.escTimer.Stop()
		l.escTimer = nil
	}
}

func (l *Loop) handle(ev input.Event) {
	switch e := ev.(type) {
	case input.CapEvent:
		l.vx.applyCap(e)
		return
	default:
		l.Post(e)
	}
}
