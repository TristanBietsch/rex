//go:build !windows

package daemonctl

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/tristanbietsch/rex/internal/wire/client"
)

// pgrepArgs matches only this user's processes named exactly rex-daemon. A
// bare `pgrep rex-daemon` also matches other users' daemons and any name
// containing it, so Stop could signal the wrong process.
func pgrepArgs() []string {
	return []string{"-u", strconv.Itoa(os.Geteuid()), "-x", "rex-daemon"}
}

// FindPID returns the PID of this user's rex-daemon (the first, if several).
func FindPID() (string, error) {
	out, err := exec.Command("pgrep", pgrepArgs()...).Output()
	if err != nil {
		return "", ErrNotRunning
	}
	pid := strings.TrimSpace(strings.Split(string(out), "\n")[0])
	if pid == "" {
		return "", ErrNotRunning
	}
	return pid, nil
}

// signalDaemon sends sig to the process FindPID finds. A var so tests never
// signal a real daemon.
var signalDaemon = func(sig syscall.Signal) error {
	pid, err := FindPID()
	if err != nil {
		return err
	}
	n, err := strconv.Atoi(pid)
	if err != nil {
		return ErrNotRunning
	}
	if err := syscall.Kill(n, sig); err != nil {
		return fmt.Errorf("signal rex-daemon (pid %d): %w", n, err)
	}
	return nil
}

// Reload asks the running rex-daemon to re-read tools.yaml: the Reload
// intent over the socket, which also reports a failed reload. SIGHUP is the
// fallback for a daemon off the default socket or too old for the intent.
func Reload() error {
	err := sendIntent(DefaultSocket(), (*client.Client).Reload)
	var ie *intentError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &ie) && !ie.unsupported():
		return err
	}
	slog.Debug("daemonctl: reload intent failed; sending SIGHUP", "err", err)
	return signalDaemon(syscall.SIGHUP)
}

// Stop asks the running rex-daemon to shut down with the Shutdown intent and
// waits up to StartTimeout for its socket to go away, so a restart can start
// a new one. SIGTERM is the fallback for a daemon off the default socket,
// too old for the intent, or still up after the wait.
func Stop() error {
	socket := DefaultSocket()
	err := sendIntent(socket, (*client.Client).Shutdown)
	if err == nil && waitSocketGone(socket) {
		return nil
	}
	slog.Debug("daemonctl: shutdown intent did not stop the daemon; sending SIGTERM", "err", err)
	return signalDaemon(syscall.SIGTERM)
}

// StopQuietly stops a running daemon; ignores errors.
func StopQuietly() { _ = Stop() }
