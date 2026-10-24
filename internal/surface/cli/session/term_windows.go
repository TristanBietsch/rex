//go:build windows

package session

import (
	"os"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

// resizePollInterval is how often the console size is checked. Windows has
// no SIGWINCH, so attach polls.
const resizePollInterval = 250 * time.Millisecond

// onResize calls fn whenever the console size changes until stop is called.
func onResize(fn func()) (stop func()) {
	done := make(chan struct{})
	go func() {
		fd := int(os.Stdout.Fd())
		lastW, lastH, _ := term.GetSize(fd)
		t := time.NewTicker(resizePollInterval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				w, h, err := term.GetSize(fd)
				if err != nil || (w == lastW && h == lastH) {
					continue
				}
				lastW, lastH = w, h
				fn()
			}
		}
	}()
	return func() { close(done) }
}

// enableVT turns on VT escape processing for console output f, so the
// agent's screen renders instead of printing raw escapes. restore puts the
// previous console mode back.
func enableVT(f *os.File) (restore func()) {
	h := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return func() {}
	}
	_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.DISABLE_NEWLINE_AUTO_RETURN)
	return func() { _ = windows.SetConsoleMode(h, mode) }
}
