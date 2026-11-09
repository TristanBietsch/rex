package daemonctl

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"time"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Stop and Reload talk to the daemon through its socket on every platform,
// as Shutdown and Reload intents. They never look processes up or signal
// them: `pgrep rex-daemon` matched other users' processes and unrelated
// daemons (backlog S-08), and Windows cannot deliver SIGTERM/SIGHUP at all.
// Whoever can connect to the socket already controls the daemon, so the
// socket is the right authority.

// Stop asks the daemon on the default socket (paths.Socket) to shut down;
// see ShutdownDaemon.
func Stop() error {
	socket, err := paths.Socket()
	if err != nil {
		return err
	}
	return ShutdownDaemon(context.Background(), socket)
}

// StopQuietly stops the daemon on the default socket if one answers; errors
// are ignored.
func StopQuietly() { _ = Stop() }

// Reload asks the daemon on the default socket (paths.Socket) to re-read
// tools.yaml; see ReloadDaemon.
func Reload() error {
	socket, err := paths.Socket()
	if err != nil {
		return err
	}
	return ReloadDaemon(context.Background(), socket)
}

// ShutdownDaemon sends a Shutdown intent to the daemon listening on socket
// and waits until the daemon has removed the socket, for at most
// StopTimeout or until ctx ends. Removing the socket is the daemon's last
// step, after its clients are gone, so once it is gone a new daemon can bind
// the same path without the old one unlinking it later. It returns
// ErrNotRunning when nothing answers on socket.
func ShutdownDaemon(ctx context.Context, socket string) error {
	return shutdownDaemon(ctx, socket, StopTimeout)
}

func shutdownDaemon(ctx context.Context, socket string, timeout time.Duration) error {
	if err := sendIntent(ctx, socket, protocol.IntentShutdown); err != nil {
		return err
	}
	return waitSocketRemoved(ctx, socket, timeout)
}

// ReloadDaemon asks the daemon listening on socket to re-read tools.yaml and
// waits, at most ReloadTimeout or until ctx ends, for its answer. A reload
// the daemon rejects (a tools.yaml it cannot load) returns the daemon's
// *client.Error, wrapped. A daemon that predates acks gets the Reload
// fire-and-forget, as before, and nil is returned: its errors still only
// reach its log. It returns ErrNotRunning when nothing answers on socket.
func ReloadDaemon(ctx context.Context, socket string) error {
	ctx, cancel := context.WithTimeout(ctx, ReloadTimeout)
	defer cancel()
	dialCtx, cancelDial := context.WithTimeout(ctx, SocketDialTimeout)
	defer cancelDial()
	c, err := client.DialContext(dialCtx, socket)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNotRunning, err)
	}
	defer c.Close()
	if _, err := c.HelloContext(ctx, "rex-reload"); err != nil {
		return fmt.Errorf("reload rex-daemon: %w", err)
	}
	if c.DaemonVersion() == "" {
		if err := c.Send(ctx, protocol.IntentReload, struct{}{}); err != nil {
			return fmt.Errorf("send %s to rex-daemon: %w", protocol.IntentReload, err)
		}
		return nil
	}
	if _, err := c.Do(ctx, protocol.IntentReload, struct{}{}); err != nil {
		return fmt.Errorf("rex-daemon could not reload tools.yaml: %w", err)
	}
	return nil
}

// sendIntent dials socket and writes one intent with an empty payload. The
// dial is bounded by SocketDialTimeout and the write by intentWriteTimeout,
// so a wedged daemon cannot hang the caller.
func sendIntent(ctx context.Context, socket, intent string) error {
	dialer := net.Dialer{Timeout: SocketDialTimeout}
	conn, err := dialer.DialContext(ctx, "unix", socket)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNotRunning, err)
	}
	defer conn.Close()
	if err := conn.SetWriteDeadline(time.Now().Add(intentWriteTimeout)); err != nil {
		return fmt.Errorf("send %s to rex-daemon: %w", intent, err)
	}
	if err := protocol.NewWriter(conn).WriteIntent(intent, "", struct{}{}); err != nil {
		return fmt.Errorf("send %s to rex-daemon: %w", intent, err)
	}
	return nil
}

// waitSocketRemoved polls until socket no longer exists. At the deadline it
// says whether the daemon still answers or stopped without removing the
// socket (it crashed during shutdown).
func waitSocketRemoved(ctx context.Context, socket string, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	tick := time.NewTicker(StartPollInterval)
	defer tick.Stop()
	for {
		if _, err := os.Lstat(socket); errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for rex-daemon to stop: %w", ctx.Err())
		case <-deadline.C:
			if Reachable(socket) {
				return fmt.Errorf("rex-daemon on %s still answering %s after shutdown was requested", socket, timeout)
			}
			return fmt.Errorf("rex-daemon on %s stopped answering but did not remove its socket within %s", socket, timeout)
		case <-tick.C:
		}
	}
}
