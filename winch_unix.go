//go:build unix

package xui

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/pulseaiclub/xui/input"
)

func (vx *XUI) startWinchSignals(loop *Loop) {
	vx.stopWinchSignals()
	winchCh := make(chan os.Signal, 1)
	stop := make(chan struct{})
	vx.winchCh = winchCh
	vx.stopWinch = stop
	signal.Notify(winchCh, syscall.SIGWINCH)
	go func() {
		for {
			select {
			case <-winchCh:
				cols, rows, err := vx.tty.Size()
				if err != nil {
					continue
				}
				xpix, ypix, _ := vx.tty.PixelSize()
				loop.Post(input.ResizeEvent{Cols: cols, Rows: rows, XPixel: xpix, YPixel: ypix})
			case <-stop:
				signal.Stop(winchCh)
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
	if vx.winchCh != nil {
		signal.Stop(vx.winchCh)
		vx.winchCh = nil
	}
}
