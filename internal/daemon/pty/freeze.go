package pty

import (
	"context"
	"errors"
	"os"
	"sync"
	"syscall"
	"time"
)

// Frozen is a session held still for a daemon hot restart (R20): nothing
// is read from or written to its PTY until it is thawed or the process
// image is replaced. Every byte read from the PTY before the freeze was
// fully handled (transcript, fan-out, screen); bytes the agent writes
// after it stay in the kernel's PTY buffer for the next image.
type Frozen struct {
	// FD is the PTY master's descriptor; PID the agent's process id.
	FD  int
	PID int
	// Cols and Rows are the PTY size.
	Cols, Rows uint16
	// Screen is the emulated screen as escape sequences: the terminal
	// modes (ModePreamble), then a full repaint (Repaint).
	Screen []byte
}

// FreezeFunc freezes a session. thaw resumes it; it is safe to call more
// than once. An error leaves the session running.
type FreezeFunc func(ctx context.Context) (f Frozen, thaw func(), err error)

// Adopted is a running child handed over by the previous daemon image:
// Run supervises it instead of spawning a new one.
type Adopted struct {
	// File is the PTY master, PID the agent (still this process's child).
	File *os.File
	PID  int
	// Screen is replayed into the fresh emulator before any new output
	// (Frozen.Screen from the previous image).
	Screen []byte
}

// errStarting refuses a freeze while the initial prompt is still waiting
// to be typed: the goroutine that would type it does not survive the exec.
var errStarting = errors.New("session is still starting (initial prompt not typed yet); retry in a few seconds")

// errFrozen refuses a resize while the session is frozen for a hot restart.
var errFrozen = errors.New("session is frozen for a daemon restart")

// readyWait is how long the reader waits for readability before it checks
// for a freeze again.
const readyWait = 50 * time.Millisecond

// gate parks the session's loops (reader, input forwarder, ticker) during
// a freeze. Each loop calls park at a point where it holds no data.
type gate struct {
	mu   sync.Mutex
	want bool
	// thaw is closed when the freeze ends.
	thaw chan struct{}
	// parked counts loops that reached park since the freeze began; its
	// channel is signaled on every arrival.
	parked  map[string]bool
	arrived chan struct{}
	// wakeCh is closed when a freeze is requested (a loop blocked in a
	// select hears it); a fresh one is made on release.
	wakeCh chan struct{}
}

func newGate() *gate { return &gate{arrived: make(chan struct{}, 8), wakeCh: make(chan struct{})} }

// wake is closed once a freeze is requested.
func (g *gate) wake() <-chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.wakeCh
}

// thawCh is closed when the current freeze ends (nil when none is active).
func (g *gate) thawCh() <-chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.thaw
}

// request starts a freeze. It returns false when one is already active.
func (g *gate) request() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.want {
		return false
	}
	g.want = true
	g.thaw = make(chan struct{})
	g.parked = map[string]bool{}
	close(g.wakeCh)
	return true
}

// release ends the freeze; parked loops go on.
func (g *gate) release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.want {
		return
	}
	g.want = false
	close(g.thaw)
	g.wakeCh = make(chan struct{})
}

// active reports whether a freeze is requested.
func (g *gate) active() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.want
}

// park blocks the calling loop (named who) while a freeze is active, until
// it is released or stop closes. It returns at once when none is active.
func (g *gate) park(who string, stop <-chan struct{}) {
	g.mu.Lock()
	if !g.want {
		g.mu.Unlock()
		return
	}
	thaw := g.thaw
	g.parked[who] = true
	g.mu.Unlock()
	select {
	case g.arrived <- struct{}{}:
	default:
	}
	select {
	case <-thaw:
	case <-stop:
	}
}

// waitParked waits until every loop in who has parked, or ctx ends.
func (g *gate) waitParked(ctx context.Context, who ...string) error {
	for {
		g.mu.Lock()
		all := g.want
		for _, w := range who {
			all = all && g.parked[w]
		}
		g.mu.Unlock()
		if all {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-g.arrived:
		case <-time.After(readyWait):
		}
	}
}

// fdOf is the console's descriptor, when it has one. It is read through
// SyscallConn, never os.File.Fd, which would switch the descriptor to
// blocking mode (Close would no longer interrupt a read or write).
func fdOf(c console) (int, bool) {
	sc, ok := c.(interface {
		SyscallConn() (syscall.RawConn, error)
	})
	if !ok {
		return -1, false
	}
	rc, err := sc.SyscallConn()
	if err != nil {
		return -1, false
	}
	fd := -1
	if err := rc.Control(func(u uintptr) { fd = int(u) }); err != nil || fd < 0 { //nolint:gosec // G115: descriptors fit an int.
		return -1, false
	}
	return fd, true
}
