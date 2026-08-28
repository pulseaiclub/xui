//go:build windows

package xui

import (
	"time"

	"github.com/pulseaiclub/xui/input"
)

// Windows has no SIGWINCH, and Windows Terminal does not implement in-band
// resize (mode 2048), so poll the console size instead. One
// GetConsoleScreenBufferInfo call per tick is cheap; 200ms keeps drag-resize
// feeling instant. Only real size changes are posted, so apps that clear and
// repaint on every ResizeEvent are not woken needlessly.
func (vx *XUI) startWinchSignals(loop *Loop) {
	vx.stopWinchSignals()
	stop := make(chan struct{})
	vx.stopWinch = stop
	go func() {
		cols, rows, _ := vx.tty.Size()
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c, r, err := vx.tty.Size()
				if err != nil || (c == cols && r == rows) {
					continue
				}
				cols, rows = c, r
				loop.Post(input.ResizeEvent{Cols: c, Rows: r})
			case <-stop:
				return
			}
		}
	}()
}

func (vx *XUI) stopWinchSignals() {
	if vx.stopWinch != nil {
		select {
		case <-vx.stopWinch:
		default:
			close(vx.stopWinch)
		}
		vx.stopWinch = nil
	}
}
