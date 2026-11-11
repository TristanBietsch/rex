package tui

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
)

// Keep-awake for the screensaver (screensaver_keep_awake).
//
// On macOS, while the screensaver shows, rex runs
//
//	/usr/bin/caffeinate -d -w <rex pid>
//
// which holds an assertion that stops the display from sleeping. It needs no
// root. The assertion is released three ways, so it can't outlive its
// purpose: rex kills caffeinate when the screensaver goes away, Run stops it
// when the TUI exits however it ends, and -w makes caffeinate exit by itself
// once rex's process is gone (crash, kill -9). On other systems it does
// nothing.

// caffeinatePath is absolute so a caffeinate earlier on PATH can't stand in.
const caffeinatePath = "/usr/bin/caffeinate"

// awakeLauncher starts the keep-awake helper and returns the function that
// stops it. Tests swap it so no real process ever runs.
type awakeLauncher func() (stop func(), err error)

// displayAwake holds the display awake on request. Model copies share one
// pointer. Start and Stop are idempotent and safe on a nil receiver.
type displayAwake struct {
	mu     sync.Mutex
	launch awakeLauncher // nil: unsupported here, Start does nothing
	stop   func()        // non-nil while the helper runs
}

// newDisplayAwake returns the keep-awake for this system: caffeinate on
// macOS, a no-op elsewhere.
func newDisplayAwake() *displayAwake {
	d := &displayAwake{}
	if runtime.GOOS == "darwin" {
		d.launch = launchCaffeinate
	}
	return d
}

// launchCaffeinate starts caffeinate for this process. Canceling the
// context kills it; a goroutine reaps it whenever it exits.
func launchCaffeinate() (func(), error) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, caffeinatePath, caffeinateArgs(os.Getpid())...)
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	go func() { _ = cmd.Wait() }()
	return cancel, nil
}

// caffeinateArgs keeps the display awake (-d) until process pid exits (-w).
func caffeinateArgs(pid int) []string {
	return []string{"-d", "-w", strconv.Itoa(pid)}
}

// Start holds the display awake unless it already is. A helper that fails
// to start is logged and skipped; the screensaver shows anyway.
func (d *displayAwake) Start() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stop != nil || d.launch == nil {
		return
	}
	stop, err := d.launch()
	if err != nil {
		slog.Warn("tui: keep-awake did not start", "err", err)
		return
	}
	d.stop = stop
	slog.Info("tui: keep-awake on")
}

// Stop lets the display sleep again. It does nothing when not holding.
func (d *displayAwake) Stop() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stop == nil {
		return
	}
	d.stop()
	d.stop = nil
	slog.Info("tui: keep-awake off")
}

// holding reports whether the helper runs.
func (d *displayAwake) holding() bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stop != nil
}
