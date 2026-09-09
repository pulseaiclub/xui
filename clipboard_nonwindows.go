//go:build !windows

package xui

import "errors"

// writeWindowsClipboard is only reachable on Windows; keep other builds compiling.
func writeWindowsClipboard(string) error {
	return errors.New("xui: Windows clipboard backend is unavailable on this platform")
}
