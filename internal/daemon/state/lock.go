package state

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// LockFileName is the file under the state root that a running daemon holds
// an exclusive lock on for its whole lifetime.
const LockFileName = "daemon.lock"

// ErrLocked matches the error AcquireLock returns when another process
// holds the lock (use errors.Is; errors.As with *LockedError gives detail).
var ErrLocked = errors.New("state dir is locked by another process")

// LockedError reports the lock file and the holder's pid.
type LockedError struct {
	Path string
	// PID is the holder's pid as recorded in the lock file; 0 when the file
	// does not hold a readable pid (the holder may still be writing it).
	PID int
}

func (e *LockedError) Error() string {
	holder := "unknown pid"
	if e.PID > 0 {
		holder = "pid " + strconv.Itoa(e.PID)
	}
	return fmt.Sprintf("%s is held by another process (%s)", e.Path, holder)
}

// Is makes errors.Is(err, ErrLocked) match a *LockedError.
func (e *LockedError) Is(target error) bool { return target == ErrLocked }

// Lock is an exclusive lock on <root>/daemon.lock. Two daemons on one state
// dir would reload each other's live sessions as crashed and race on every
// meta.json, so a daemon takes this before it reads any state and keeps it
// until it exits. The lock is advisory (flock on Unix, LockFileEx on
// Windows) and the OS drops it when the process dies, so a crash never
// leaves a stale lock; the file itself is left in place.
type Lock struct {
	f    *os.File
	path string
}

// AcquireLock takes the state-dir lock without blocking and records this
// process's pid in it. root must exist. If another process holds the lock
// it returns a *LockedError (matching ErrLocked).
func AcquireLock(root string) (*Lock, error) {
	path := filepath.Join(root, LockFileName)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	if err := lockFile(f); err != nil {
		pid := readLockPID(f)
		_ = f.Close()
		if errors.Is(err, errWouldBlock) {
			return nil, &LockedError{Path: path, PID: pid}
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	if err := writeLockPID(f, os.Getpid()); err != nil {
		_ = unlockFile(f)
		_ = f.Close()
		return nil, fmt.Errorf("record pid in %s: %w", path, err)
	}
	return &Lock{f: f, path: path}, nil
}

// Path is the lock file's path.
func (l *Lock) Path() string { return l.path }

// Release drops the lock. Call it once, after the daemon has stopped
// touching the state dir.
func (l *Lock) Release() error {
	uerr := unlockFile(l.f)
	cerr := l.f.Close()
	if err := errors.Join(uerr, cerr); err != nil {
		return fmt.Errorf("release %s: %w", l.path, err)
	}
	return nil
}

// errWouldBlock is what lockFile returns when another process holds the lock.
var errWouldBlock = errors.New("lock held")

func writeLockPID(f *os.File, pid int) error {
	if err := f.Truncate(0); err != nil {
		return fmt.Errorf("truncate: %w", err)
	}
	if _, err := f.WriteAt([]byte(strconv.Itoa(pid)+"\n"), 0); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync: %w", err)
	}
	return nil
}

// readLockPID returns the pid recorded in f, or 0 if there is none.
func readLockPID(f *os.File) int {
	b, err := io.ReadAll(io.NewSectionReader(f, 0, 32))
	if err != nil {
		return 0
	}
	field, _, _ := strings.Cut(strings.TrimSpace(string(b)), " ")
	pid, err := strconv.Atoi(field)
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}
