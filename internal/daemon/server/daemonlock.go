package server

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// ErrAlreadyRunning is returned by New when another daemon is serving the
// socket or holds the state dir.
var ErrAlreadyRunning = errors.New("rex-daemon is already running")

// stateLockName is the file in the state dir a daemon holds an exclusive lock
// on for its whole life, so two daemons never share one state dir (the second
// would reload the first's live sessions as crashed and its retire sweep
// could delete their directories).
const stateLockName = "daemon.lock"

// stateLockWait is how long New waits for a held state lock: a daemon that is
// shutting down (rex daemon restart) still holds it while it reaps sessions.
var stateLockWait = 6 * time.Second

// errLocked is what lockFile returns when another process holds the lock.
var errLocked = errors.New("locked by another process")

// claimDaemon makes this process the daemon for cfg: refuse when a daemon
// answers on the socket, take the state-dir lock, then clear a stale socket
// file. The returned lock (nil without a state dir) is held until Serve ends.
func claimDaemon(cfg Config) (*os.File, error) {
	if socketLive(cfg.Socket) {
		return nil, fmt.Errorf("%w (socket %s accepts connections)", ErrAlreadyRunning, cfg.Socket)
	}
	var lock *os.File
	if cfg.StateDir != "" {
		var err error
		if lock, err = acquireStateLock(cfg.StateDir); err != nil {
			return nil, err
		}
	}
	// A daemon with a different state dir may have bound the socket meanwhile.
	if socketLive(cfg.Socket) {
		releaseStateLock(lock)
		return nil, fmt.Errorf("%w (socket %s accepts connections)", ErrAlreadyRunning, cfg.Socket)
	}
	// Nobody answers: whatever is at the path is a stale socket file.
	_ = os.Remove(cfg.Socket)
	return lock, nil
}

// socketLive reports whether something accepts connections on path.
func socketLive(path string) bool {
	c, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// acquireStateLock takes the exclusive lock on <dir>/daemon.lock, waiting up
// to stateLockWait for a previous daemon to let go of it.
func acquireStateLock(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("state dir: %w", err)
	}
	path := filepath.Join(dir, stateLockName)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	deadline := time.Now().Add(stateLockWait)
	logged := false
	for {
		err = lockFile(f)
		if err == nil {
			break
		}
		if !errors.Is(err, errLocked) {
			_ = f.Close()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("%w (state dir %s is locked by another daemon)", ErrAlreadyRunning, dir)
		}
		if !logged {
			slog.Info("server: state dir locked; waiting for the other daemon to exit", "lock", path, "wait", stateLockWait)
			logged = true
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Record the holder for whoever inspects the file; the lock is what counts.
	if err := f.Truncate(0); err == nil {
		_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())+"\n"), 0)
	}
	return f, nil
}

// releaseStateLock drops the lock (closing the file releases it).
func releaseStateLock(f *os.File) {
	if f != nil {
		_ = f.Close()
	}
}

// removeOwnSocket unlinks path only if it is still the socket this server
// bound (ours): after we stop listening, a newer daemon may own the path.
func removeOwnSocket(path string, ours os.FileInfo) {
	if ours == nil {
		return
	}
	cur, err := os.Lstat(path)
	if err != nil {
		return
	}
	if !os.SameFile(ours, cur) {
		slog.Info("server: socket path now belongs to another daemon; leaving it", "socket", path)
		return
	}
	_ = os.Remove(path)
}
