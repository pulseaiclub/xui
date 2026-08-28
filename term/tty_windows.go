//go:build windows

package term

import (
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode             = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode             = kernel32.NewProc("SetConsoleMode")
	procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
	procSetConsoleModeOut          = kernel32.NewProc("SetConsoleMode")
)

const (
	enableProcessedInput       = 0x1
	enableLineInput            = 0x2
	enableEchoInput            = 0x4
	enableWindowInput          = 0x8
	enableMouseInput           = 0x10
	enableInsertMode           = 0x20
	enableQuickEditMode        = 0x40
	enableExtendedFlags        = 0x80
	enableVirtualTerminalInput = 0x200
	enableProcessedOutput      = 0x1
	enableWrapAtEolOutput      = 0x2
	enableVirtualTerminalProc  = 0x4
)

func openTTY() (TTY, error) {
	return &winTTY{in: os.Stdin, out: os.Stdout}, nil
}

type winTTY struct {
	in, out *os.File
	inMode  uint32
	outMode uint32
	rawSet  bool
}

func (t *winTTY) Read(p []byte) (int, error)  { return t.in.Read(p) }
func (t *winTTY) Write(p []byte) (int, error) { return t.out.Write(p) }
func (t *winTTY) Fd() uintptr                 { return t.in.Fd() }

type coord struct{ X, Y int16 }
type smallRect struct{ Left, Top, Right, Bottom int16 }
type consoleScreenBufferInfo struct {
	Size       coord
	CursorPos  coord
	Attributes uint16
	Window     smallRect
	MaxSize    coord
}

func (t *winTTY) Size() (cols, rows int, err error) {
	var info consoleScreenBufferInfo
	h := t.out.Fd()
	r, _, e := procGetConsoleScreenBufferInfo.Call(h, uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 80, 24, e
	}
	cols = int(info.Window.Right - info.Window.Left + 1)
	rows = int(info.Window.Bottom - info.Window.Top + 1)
	if cols <= 0 || rows <= 0 {
		return 80, 24, nil
	}
	return cols, rows, nil
}

// PixelSize is unsupported on the Windows console; graphics protocols
// (kitty/sixel) are unavailable there.
func (t *winTTY) PixelSize() (xpix, ypix int, err error) {
	return 0, 0, nil
}

func (t *winTTY) MakeRaw() error {
	var mode uint32
	hIn := t.in.Fd()
	r, _, e := procGetConsoleMode.Call(hIn, uintptr(unsafe.Pointer(&mode)))
	if r == 0 {
		return fmt.Errorf("GetConsoleMode: %v", e)
	}
	t.inMode = mode
	mode &^= enableEchoInput | enableLineInput | enableProcessedInput | enableQuickEditMode
	// ENABLE_EXTENDED_FLAGS must be set for the QuickEdit change to stick;
	// leaving QuickEdit on lets a stray mouse click put the console into
	// selection mode, which blocks all I/O and freezes the app.
	mode |= enableVirtualTerminalInput | enableWindowInput | enableExtendedFlags
	r, _, e = procSetConsoleMode.Call(hIn, uintptr(mode))
	if r == 0 {
		return fmt.Errorf("SetConsoleMode in: %v", e)
	}

	hOut := t.out.Fd()
	r, _, e = procGetConsoleMode.Call(hOut, uintptr(unsafe.Pointer(&mode)))
	if r == 0 {
		return fmt.Errorf("GetConsoleMode out: %v", e)
	}
	t.outMode = mode
	// xui paints with absolute cursor positioning and never relies on
	// auto-wrap. Leave ENABLE_WRAP_AT_EOL_OUTPUT off: on the legacy Windows
	// console (conhost) — unlike xterm-likes, which defer the wrap — writing
	// the bottom-right cell wraps immediately and scrolls the whole buffer
	// up one line, tearing every full-height frame.
	mode |= enableVirtualTerminalProc | enableProcessedOutput
	mode &^= enableWrapAtEolOutput
	r, _, e = procSetConsoleMode.Call(hOut, uintptr(mode))
	if r == 0 {
		return fmt.Errorf("SetConsoleMode out: %v", e)
	}
	t.rawSet = true
	return nil
}

func (t *winTTY) Restore() error {
	if !t.rawSet {
		return nil
	}
	_, _, _ = procSetConsoleMode.Call(t.in.Fd(), uintptr(t.inMode))
	_, _, _ = procSetConsoleMode.Call(t.out.Fd(), uintptr(t.outMode))
	return nil
}

func (t *winTTY) Close() error {
	return t.Restore()
}

func (t *winTTY) Interrupt() {
	_ = t.in.SetReadDeadline(time.Now())
}

func (t *winTTY) ClearDeadline() {
	_ = t.in.SetReadDeadline(time.Time{})
}
