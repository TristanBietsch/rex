//go:build windows

package core

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableVT turns on VT escape processing for console output f. restore puts
// the previous console mode back.
func enableVT(f *os.File) (restore func()) {
	h := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return func() {}
	}
	_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	return func() { _ = windows.SetConsoleMode(h, mode) }
}
