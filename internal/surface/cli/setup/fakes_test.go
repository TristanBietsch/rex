package setup

import (
	"io"
	"sync"
	"testing"
	"time"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// installPrinter sets the process Printer for a test: with interactive, the
// text format with both terminals (so Confirm asks); otherwise txt on pipes.
// Streams are discarded; commands with injected writers use core's WithIO.
func installPrinter(interactive bool) {
	env := core.Env{
		Getenv: func(k string) string {
			if k == "NO_COLOR" {
				return "1"
			}
			return ""
		},
		InTTY: interactive, ErrTTY: interactive, Width: 80, Scheme: "default", GOOS: "linux",
	}
	fl := core.FormatFlags{}
	if interactive {
		fl.Output = "text"
	}
	p, err := core.NewPrinter(fl, env, io.Discard, io.Discard)
	if err != nil {
		panic(err)
	}
	core.SetOut(p)
}

// fakeDaemon stands in for rex-daemon in update and uninstall tests, so no
// test ever signals a real process. Its socket answers while up; stop
// takes it down unless stubborn.
type fakeDaemon struct {
	mu       sync.Mutex
	up       bool
	stubborn bool  // stop "succeeds" but the daemon keeps answering
	stopErr  error // returned by stop
	stops    int
	probes   int
	// stoppedSockets records the socket each stop request was sent to.
	stoppedSockets []string
	// downAfter, when > 0, keeps the socket answering for that many probes
	// after stop, like a daemon that takes a moment to persist and exit.
	downAfter int
	// onStop runs inside stop, before the daemon goes down. Tests use it to
	// check what still exists at the moment the daemon is stopped.
	onStop func()
	// log, when set, records "stop" in call order alongside other fakes.
	log *[]string
}

func (f *fakeDaemon) control() daemonControl {
	return daemonControl{
		socket:    "fake.sock",
		stop:      f.stop,
		reachable: f.reachable,
		timeout:   50 * time.Millisecond,
		interval:  time.Millisecond,
	}
}

func (f *fakeDaemon) stop(socket string) error {
	if f.onStop != nil {
		f.onStop()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	f.stoppedSockets = append(f.stoppedSockets, socket)
	if f.log != nil {
		*f.log = append(*f.log, "stop")
	}
	if f.stopErr != nil {
		return f.stopErr
	}
	if !f.stubborn && f.downAfter == 0 {
		f.up = false
	}
	return nil
}

func (f *fakeDaemon) reachable(string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.probes++
	if f.up && f.stops > 0 && f.downAfter > 0 && !f.stubborn {
		f.downAfter--
		if f.downAfter == 0 {
			f.up = false
		}
		return true
	}
	return f.up
}

func (f *fakeDaemon) stopCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stops
}

// atTerminal installs a text Printer with stdin, stdout and stderr all
// terminals: a person at a terminal, the only one who may change a trust
// setting from the CLI. Output is discarded.
func atTerminal(t *testing.T) {
	t.Helper()
	atFakeTTY(t, core.Env{InTTY: true, OutTTY: true, ErrTTY: true})
}

// atFakeTTY installs a text Printer with the terminals env names.
func atFakeTTY(t *testing.T, env core.Env) {
	t.Helper()
	env.Getenv, env.Width, env.Scheme, env.GOOS = func(string) string { return "" }, 80, "default", "linux"
	p, err := core.NewPrinter(core.FormatFlags{NoColor: true}, env, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	core.SetOut(p)
	t.Cleanup(func() { core.SetOut(nil) })
}
