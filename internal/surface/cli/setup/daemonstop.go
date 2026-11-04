package setup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// daemonStopTimeout bounds how long update and uninstall wait for the
// daemon's socket to stop answering after the stop request returns.
// shutdownDaemon itself already waits up to daemonctl.StopTimeout for the
// daemon to remove its socket; this covers a daemon that keeps answering
// after that.
const daemonStopTimeout = 5 * time.Second

// errDaemonStillRunning means the daemon's socket still accepted
// connections after the stop timeout.
var errDaemonStillRunning = errors.New("rex-daemon is still running")

// daemonControl stops the rex-daemon on socket and confirms it is gone.
//
// The socket is the authority: stop sends a Shutdown intent to whatever
// answers on it, and running probes it. No process is looked up by name or
// signaled. stop and reachable are shutdownDaemon and daemonctl.Reachable
// in production; they are fields so tests drive a fake daemon and never
// reach a real one.
type daemonControl struct {
	socket    string
	stop      func(socket string) error
	reachable func(socket string) bool
	timeout   time.Duration
	interval  time.Duration
}

// defaultDaemonControl targets the daemon on the default socket.
func defaultDaemonControl() daemonControl {
	return daemonControl{
		socket:    core.DefaultSocket(),
		stop:      shutdownDaemon,
		reachable: daemonctl.Reachable,
		timeout:   daemonStopTimeout,
		interval:  daemonctl.StartPollInterval,
	}
}

// shutdownDaemon is daemonctl.ShutdownDaemon without a caller context:
// update and uninstall have none, and ShutdownDaemon bounds its own wait.
func shutdownDaemon(socket string) error {
	return daemonctl.ShutdownDaemon(context.Background(), socket)
}

// running reports whether the daemon's socket accepts connections.
func (d daemonControl) running() bool { return d.reachable(d.socket) }

// stopAndWait asks the daemon on d.socket to stop, then polls the socket
// until it stops answering, so update and uninstall never act while the
// daemon may still be writing state.
//
// It returns nil once the socket is unreachable, including when no daemon
// was running. Otherwise it returns an error wrapping errDaemonStillRunning
// and, if the stop request itself failed, that error too.
func (d daemonControl) stopAndWait() error {
	stopErr := d.stop(d.socket)
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
