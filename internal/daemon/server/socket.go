package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"time"
)

// ErrSocketInUse reports that a live process answers on the socket path, so
// Serve refuses to replace it.
var ErrSocketInUse = errors.New("socket is in use by a running daemon")

// staleProbeTimeout bounds the dial that tells a live socket from a stale one.
const staleProbeTimeout = time.Second

// listenSocket claims path and listens on it. A socket file left by a dead
// daemon is replaced; a path a live process answers on is refused with
// ErrSocketInUse, and a path that is not a socket is never touched. The
// returned FileInfo identifies the socket this listener created, for
// removeOwnSocket. Unlink-on-close is off: removal is removeOwnSocket's job.
func listenSocket(ctx context.Context, path string) (*net.UnixListener, os.FileInfo, error) {
	if err := clearStaleSocket(ctx, path); err != nil {
		return nil, nil, err
	}
	var lc net.ListenConfig
	l, err := lc.Listen(ctx, "unix", path)
	if err != nil {
		return nil, nil, fmt.Errorf("listen %s: %w", path, err)
	}
	ul, ok := l.(*net.UnixListener)
	if !ok {
		_ = l.Close()
		return nil, nil, fmt.Errorf("listen %s: unexpected listener %T", path, l)
	}
	ul.SetUnlinkOnClose(false)
	ours, err := os.Lstat(path)
	if err != nil {
		_ = ul.Close()
		return nil, nil, fmt.Errorf("stat socket %s: %w", path, err)
	}
	// On Windows, os.SameFile loads a FileInfo's file id lazily from its
	// path; compare once now so ours holds this socket's id, not that of
	// whatever sits at the path when the daemon exits.
	_ = os.SameFile(ours, ours)
	return ul, ours, nil
}

// clearStaleSocket removes a socket file nobody answers on. A missing path
// is fine; a non-socket or a live socket is an error.
func clearStaleSocket(ctx context.Context, path string) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat socket %s: %w", path, err)
	}
	if fi.Mode().Type() != fs.ModeSocket {
		return fmt.Errorf("%s exists and is not a socket; refusing to replace it", path)
	}
	d := net.Dialer{Timeout: staleProbeTimeout}
	if conn, err := d.DialContext(ctx, "unix", path); err == nil {
		_ = conn.Close()
		return fmt.Errorf("%w: %s", ErrSocketInUse, path)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove stale socket %s: %w", path, err)
	}
	slog.Info("server: removed stale socket", "socket", path)
	return nil
}

// removeOwnSocket unlinks path only if it is still the socket this daemon
// created. If another daemon has since replaced it, it is left alone.
func removeOwnSocket(path string, ours os.FileInfo) {
	cur, err := os.Lstat(path)
	if err != nil {
		return
	}
	if !os.SameFile(ours, cur) {
		slog.Warn("server: socket path was replaced by another process; leaving it", "socket", path)
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("server: remove socket", "socket", path, "err", err)
	}
}
