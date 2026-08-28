package xui

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pulseaiclub/xui/cell"
	"github.com/pulseaiclub/xui/graphics"
	"github.com/pulseaiclub/xui/input"
	"github.com/pulseaiclub/xui/render"
	"github.com/pulseaiclub/xui/screen"
	"github.com/pulseaiclub/xui/term"
)

// Options configures XUI.
type Options struct {
	Mouse          bool
	BracketedPaste bool
}

// XUI is the core TUI engine: screen, capabilities, and render.
type XUI struct {
	tty      term.TTY
	screen   *screen.Screen
	renderer *render.Renderer
	caps     render.Caps
	opts     Options

	altScreen   bool
	kittyPushed bool
	mu          sync.Mutex

	// colorForced is true when the user set a hard color ceiling
	// (FORCE_COLOR numeric / NO_COLOR / TERM=dumb / SetColorLevel).
	// In-band probes must not raise ColorLevel past that intent.
	colorForced bool

	// graphicsNext accumulates image placements drawn since the last Render;
	// graphicsLast remembers what was written. Both are owned by the UI
	// goroutine (Draw + Render), the same one that touches screen.
	graphicsNext []*graphics.Placement
	graphicsLast []*graphics.Placement
	graphicsID   uint64
	refresh      bool

	queryDone chan struct{}
	winchCh   chan os.Signal
	stopWinch chan struct{}
}

// New opens the TTY and creates a XUI instance.
func New(opts Options) (*XUI, error) {
	tty, err := term.OpenTTY()
	if err != nil {
		return nil, err
	}
	if err := tty.MakeRaw(); err != nil {
		_ = tty.Close()
		return nil, fmt.Errorf("xui: make raw: %w", err)
	}
	cols, rows, _ := tty.Size()
	vx := &XUI{
		tty:       tty,
		screen:    screen.NewScreen(cols, rows),
		renderer:  render.NewRenderer(),
		opts:      opts,
		queryDone: make(chan struct{}),
		stopWinch: make(chan struct{}),
	}
	vx.caps.ColorLevel, vx.colorForced = detectColorLevel()
	vx.renderer.UpdateCaps(vx.caps)
	return vx, nil
}

// Close restores the terminal and releases resources.
func (vx *XUI) Close() error {
	vx.stopWinchSignals()
	vx.mu.Lock()
	defer vx.mu.Unlock()
	var b strings.Builder
	if vx.opts.Mouse {
		b.WriteString(render.SeqMouseReset)
	}
	if vx.opts.BracketedPaste {
		b.WriteString(render.SeqBracketedPasteReset)
	}
	if vx.kittyPushed {
		b.WriteString(render.SeqKittyKBPop)
		vx.kittyPushed = false
	}
	b.WriteString(render.SeqModifyOtherKeysReset)
	if vx.caps.Unicode {
		b.WriteString(render.SeqUnicodeReset)
	}
	if vx.caps.InBandResize {
		b.WriteString(render.SeqInBandResizeReset)
	}
	if vx.altScreen {
		b.WriteString(render.ExitAltScreenSeq())
		vx.altScreen = false
	} else {
		b.WriteString(render.SeqSGRReset)
		b.WriteString(render.SeqShowCursor)
	}
	_, _ = vx.tty.Write([]byte(b.String()))
	return vx.tty.Close()
}

// Caps returns the current capability set.
func (vx *XUI) Caps() render.Caps {
	vx.mu.Lock()
	defer vx.mu.Unlock()
	return vx.caps
}

// Screen returns the underlying screen.
func (vx *XUI) Screen() *screen.Screen { return vx.screen }

// Window returns a full-screen Window for drawing.
func (vx *XUI) Window() screen.Window { return screen.NewWindow(vx.screen) }

// EnterAltScreen switches to the alternate screen buffer.
func (vx *XUI) EnterAltScreen() error {
	vx.mu.Lock()
	defer vx.mu.Unlock()
	if vx.altScreen {
		return nil
	}
	_, err := vx.tty.Write([]byte(render.EnterAltScreenSeq()))
	if err != nil {
		return err
	}
	vx.altScreen = true
	vx.renderer.ResetState()
	vx.screen.MarkRefresh()
	vx.refresh = true // alt screen starts empty: every placement must be re-emitted
	return nil
}

// ExitAltScreen leaves the alternate screen buffer.
func (vx *XUI) ExitAltScreen() error {
	vx.mu.Lock()
	defer vx.mu.Unlock()
	if !vx.altScreen {
		return nil
	}
	_, err := vx.tty.Write([]byte(render.ExitAltScreenSeq()))
	vx.altScreen = false
	vx.renderer.ResetState()
	return err
}

// EnableMouse turns on mouse tracking.
func (vx *XUI) EnableMouse() error {
	vx.opts.Mouse = true
	_, err := vx.tty.Write([]byte(render.SeqMouseSet))
	return err
}

// EnableBracketedPaste turns on bracketed paste.
func (vx *XUI) EnableBracketedPaste() error {
	vx.opts.BracketedPaste = true
	_, err := vx.tty.Write([]byte(render.SeqBracketedPasteSet))
	return err
}

// QueryTerminal sends capability probes and waits up to timeout for DA1.
func (vx *XUI) QueryTerminal(timeout time.Duration) {
	// Probe features first, Primary DA last so its reply arrives after
	// Kitty/DECRQM responses and we can enable what we detected.
	queries := render.SeqXTVersion +
		render.SeqKittyKBQuery +
		render.SeqKittyGraphicsQuery +
		render.SeqSixelQuery +
		render.SeqDECRQMSync +
		render.SeqDECRQMUnicode +
		render.SeqPrimaryDA
	_, _ = vx.tty.Write([]byte(queries))

	vx.mu.Lock()
	vx.queryDone = make(chan struct{})
	done := vx.queryDone
	vx.mu.Unlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	}
	vx.mu.Lock()
	vx.renderer.UpdateCaps(vx.caps)
	vx.mu.Unlock()

	vx.enableDetectedFeatures()
}

// enableDetectedFeatures turns on Kitty keyboard / unicode / mouse / paste
// based on caps (and always enables modifyOtherKeys as a Shift+Enter fallback).
func (vx *XUI) enableDetectedFeatures() {
	vx.mu.Lock()
	caps := vx.caps
	pushKitty := caps.KittyKeyboard && !vx.kittyPushed
	if pushKitty {
		vx.kittyPushed = true
	}
	vx.mu.Unlock()

	var enable strings.Builder
	if caps.Unicode {
		enable.WriteString(render.SeqUnicodeSet)
	}
	if pushKitty {
		enable.WriteString(render.SeqKittyKBPush)
	}
	// Enables modifyOtherKeys mode 2 so Shift+Enter arrives as CSI 27;2;13~
	// when Kitty keyboard is unavailable (e.g. some tmux setups).
	enable.WriteString(render.SeqModifyOtherKeysSet)
	if s := enable.String(); s != "" {
		_, _ = vx.tty.Write([]byte(s))
	}
	if vx.opts.Mouse {
		_ = vx.EnableMouse()
	}
	if vx.opts.BracketedPaste {
		_ = vx.EnableBracketedPaste()
	}
}

func (vx *XUI) applyCap(e input.CapEvent) {
	var pushKitty bool
	vx.mu.Lock()
	switch e.Kind {
	case input.CapDA1:
		// DA1 only proves the terminal answers queries, not color depth
		// (Linux console replies ESC[?6c but is 16-color). Sentinel only.
		select {
		case <-vx.queryDone:
		default:
			close(vx.queryDone)
		}
	case input.CapKittyKB:
		vx.caps.KittyKeyboard = true
		// Late reply after QueryTerminal finished: push now (once).
		if vx.queryDone != nil {
			select {
			case <-vx.queryDone:
				if !vx.kittyPushed {
					vx.kittyPushed = true
					pushKitty = true
				}
			default:
				// Still querying; enableDetectedFeatures will push.
			}
		}
	case input.CapDECRQM:
		if strings.Contains(e.Data, "?2026") && (strings.Contains(e.Data, ";1$") || strings.Contains(e.Data, ";2$")) {
			vx.caps.SyncOutput = true
		}
		if strings.Contains(e.Data, "?2027") && (strings.Contains(e.Data, ";1$") || strings.Contains(e.Data, ";2$")) {
			vx.caps.Unicode = true
		}
	case input.CapKittyGraphics:
		vx.caps.KittyGraphics = true
	case input.CapSixel:
		vx.caps.Sixel = true
	case input.CapXTVersion:
		// XTVERSION responders (xterm/kitty/wezterm/ghostty/…) are
		// truecolor-era terminals; upgrade unless the user hard-capped.
		if !vx.colorForced && vx.caps.ColorLevel < cell.ColorTrue {
			vx.caps.ColorLevel = cell.ColorTrue
		}
	}
	vx.renderer.UpdateCaps(vx.caps)
	vx.mu.Unlock()

	if pushKitty {
		_, _ = vx.tty.Write([]byte(render.SeqKittyKBPush))
	}
}

// SetColorLevel overrides the color capability (tests or unusual terminals).
// The level becomes a hard ceiling: subsequent probes will not raise it.
func (vx *XUI) SetColorLevel(level cell.ColorLevel) {
	vx.mu.Lock()
	defer vx.mu.Unlock()
	vx.caps.ColorLevel = level
	vx.colorForced = true
	vx.renderer.UpdateCaps(vx.caps)
}

// ColorLevel returns the effective color capability.
func (vx *XUI) ColorLevel() cell.ColorLevel {
	vx.mu.Lock()
	defer vx.mu.Unlock()
	return vx.caps.ColorLevel
}

// Resize updates the screen. Must be called on the main goroutine.
// Always clears the TTY and marks a full refresh: duplicate SIGWINCH with the
// same cols/rows is common (e.g. Ghostty drag end), and clearing without
// MarkRefresh leaves Diff empty → blank screen until the next forced paint.
func (vx *XUI) Resize(cols, rows int) {
	vx.mu.Lock()
	defer vx.mu.Unlock()
	vx.screen.Resize(cols, rows)
	vx.renderer.ResetState()
	_, _ = vx.tty.Write([]byte(render.SeqClearScreen + render.SeqHome))
	vx.screen.MarkRefresh()
	vx.refresh = true // the clear wiped placements; re-emit all on the next frame
}

// ResizeToTTY queries the TTY size and resizes.
func (vx *XUI) ResizeToTTY() {
	cols, rows, err := vx.tty.Size()
	if err != nil {
		return
	}
	vx.Resize(cols, rows)
}

// cellPixelSize returns the pixel size of one cell from the TTY ioctl.
// Pixel-addressed graphics (kitty/sixel) need it to size images; (0, 0)
// means the terminal reports no pixel size and block cells are used.
func (vx *XUI) cellPixelSize() (int, int) {
	xpix, ypix, err := vx.tty.PixelSize()
	if err != nil {
		return 0, 0
	}
	cols, rows := vx.screen.Size()
	if cols == 0 || rows == 0 || xpix == 0 || ypix == 0 {
		return 0, 0
	}
	return xpix / cols, ypix / rows
}

// CellPixelSize returns the pixel size of one cell; (0, 0) when unknown.
func (vx *XUI) CellPixelSize() (int, int) {
	vx.mu.Lock()
	defer vx.mu.Unlock()
	return vx.cellPixelSize()
}

// Render diffs the screen, reconciles graphics placements, and writes ANSI
// to the TTY. Placements (kitty/sixel images) live outside the cell grid, so
// they are reconciled before the cell diff: stale placements are deleted,
// new ones written, identical ones left alone.
func (vx *XUI) Render() error {
	vx.mu.Lock()
	defer vx.mu.Unlock()
	dirty := vx.screen.Diff()
	vx.graphicsLast = graphics.ReconcilePlacements(vx.tty, vx.graphicsLast, vx.graphicsNext, vx.refresh)
	vx.graphicsNext = nil
	vx.refresh = false
	cx, cy, vis, shape := vx.screen.Cursor()
	_, err := vx.renderer.RenderDiff(vx.tty, dirty, cx, cy, vis, shape)
	if err != nil {
		return err
	}
	vx.screen.Present()
	return nil
}

// QueueRefresh forces a full redraw on the next Render: the whole cell grid
// and every graphics placement are re-emitted. Placements are re-created but
// images are not re-uploaded: the terminal's graphics store survives
// clear-screen and alt-screen switches on kitty/wezterm/ghostty.
func (vx *XUI) QueueRefresh() {
	vx.refresh = true
	vx.screen.MarkRefresh()
}

// WriteRaw writes bytes directly to the TTY.
func (vx *XUI) WriteRaw(p []byte) (int, error) { return vx.tty.Write(p) }

// NotifyWinsize starts delivering ResizeEvents to loop on SIGWINCH (unix).
func (vx *XUI) NotifyWinsize(loop *Loop) { vx.startWinchSignals(loop) }
