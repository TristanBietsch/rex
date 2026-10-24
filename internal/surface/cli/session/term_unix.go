//go:build !windows

package session

import (
	"os"
	"os/signal"
	"syscall"
)

// onResize calls fn on every terminal resize (SIGWINCH) until stop is called.
func onResize(fn func()) (stop func()) {
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	go func() {
		for range winch {
			fn()
		}
	}()
	return func() { signal.Stop(winch) }
}

// enableVT is a no-op: Unix terminals always interpret escape sequences.
func enableVT(*os.File) (restore func()) { return func() {} }
