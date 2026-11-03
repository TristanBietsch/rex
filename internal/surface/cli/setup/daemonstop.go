package setup

import (
	"errors"
	"fmt"
	"time"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// daemonStopTimeout bounds how long update and uninstall wait for the
// daemon's socket to stop answering after asking it to stop. Shutdown
// persists every session and reaps PTYs, so allow more than a spawn.
const daemonStopTimeout = 5 * time.Second

// errDaemonStillRunning means the daemon's socket still accepted
// connections after the stop timeout.
var errDaemonStillRunning = errors.New("rex-daemon is still running")

// daemonControl stops the rex-daemon on socket and confirms it is gone.
//
// stop and reachable are daemonctl.Stop and daemonctl.Reachable in
// production. They are fields so tests drive a fake daemon: tests must
// never signal a real process (daemonctl.Stop finds its target by process
// name, not by socket).
type daemonControl struct {
	socket    string
	stop      func() error
	reachable func(socket string) bool
	timeout   time.Duration
	interval  time.Duration
}

// defaultDaemonControl targets the daemon on the default socket.
func defaultDaemonControl() daemonControl {
	return daemonControl{
		socket:    core.DefaultSocket(),
		stop:      daemonctl.Stop,
		reachable: daemonctl.Reachable,
		timeout:   daemonStopTimeout,
		interval:  daemonctl.StartPollInterval,
	}
}

// running reports whether the daemon's socket accepts connections.
func (d daemonControl) running() bool { return d.reachable(d.socket) }

// stopAndWait asks the daemon to stop, then polls the socket until it stops
// answering. daemonctl.Stop only sends the request, so returning from it
// does not mean the daemon has finished writing state.
//
// It returns nil once the socket is unreachable, including when no daemon
// was running. Otherwise it returns an error wrapping errDaemonStillRunning
// and, if the stop request itself failed, that error too.
func (d daemonControl) stopAndWait() error {
	stopErr := d.stop()
	if errors.Is(stopErr, daemonctl.ErrNotRunning) {
		stopErr = nil
	}
	if d.waitUnreachable() {
		return nil
	}
	if stopErr != nil {
		return fmt.Errorf("%w on %s (stop failed: %w)", errDaemonStillRunning, d.socket, stopErr)
	}
	return fmt.Errorf("%w: %s still answers after %s", errDaemonStillRunning, d.socket, d.timeout)
}

// waitUnreachable polls until the socket stops answering or the timeout
// passes, and reports which happened. It checks at least once.
func (d daemonControl) waitUnreachable() bool {
	deadline := time.Now().Add(d.timeout)
	for d.running() {
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(d.interval)
	}
	return true
}
