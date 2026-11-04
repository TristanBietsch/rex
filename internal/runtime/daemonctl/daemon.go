// Package daemonctl spawns, checks, and resolves the rex-daemon process.
// Shared between the CLI (rex daemon start) and the TUI boot splash.
package daemonctl

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"

	"github.com/tristanbietsch/rex/internal/runtime/rexlog"
)

// ErrExitedEarly is returned by Start when rex-daemon exits before its
// socket accepts connections.
var ErrExitedEarly = errors.New("rex-daemon exited before its socket came up")

// Reachable returns true when a unix socket connect succeeds quickly.
func Reachable(socket string) bool {
	conn, err := net.DialTimeout("unix", socket, SocketDialTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// DefaultSocket returns the default UDS path, matching rex-daemon's logic.
func DefaultSocket() string {
	if r := os.Getenv("XDG_RUNTIME_DIR"); r != "" {
		return filepath.Join(r, "rex.sock")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "rex", "rex.sock")
}

// FindBinary returns the path to rex-daemon. It checks (in order):
//  1. the same directory as the current rex executable,
//  2. PATH lookup,
//  3. the literal name "rex-daemon" (so exec gives a useful error).
func FindBinary() string {
	if self, err := os.Executable(); err == nil {
		if path := findBinaryIn(filepath.Dir(self)); path != "" {
			return path
		}
	}
	if path, err := exec.LookPath("rex-daemon"); err == nil {
		return path
	}
	return "rex-daemon"
}

// findBinaryIn is FindBinary's first probe, exposed for tests.
func findBinaryIn(dir string) string {
	candidate := filepath.Join(dir, "rex-daemon"+exeSuffix)
	info, err := os.Stat(candidate)
	if err == nil && !info.IsDir() {
		return candidate
	}
	return ""
}

// StartResult describes a successful daemon spawn.
type StartResult struct {
	PID     int
	Elapsed time.Duration
}

// Start spawns rex-daemon listening on socket (passed as -socket) and waits
// up to StartTimeout for the socket to accept connections. The caller
// chooses the log file for the daemon's stderr (nil discards it; OpenLog
// opens the standard one); Start tightens it to mode 0600. Returns nil
// StartResult on failure.
//
// The daemon runs detached: in its own session on Unix, its own process
// group and hidden console on Windows, so Ctrl-C in the spawning terminal
// does not reach it. Start never leaves a zombie: a goroutine waits on the
// child for as long as it runs. A child that exits before its socket comes
// up yields ErrExitedEarly with its exit status and the end of the log.
func Start(socket string, stderrLog *os.File) (*StartResult, error) {
	return start(FindBinary(), socket, stderrLog)
}

// start is Start with the daemon binary chosen by the caller (tests run
// themselves as a fake daemon).
func start(bin, socket string, stderrLog *os.File) (*StartResult, error) {
	if err := ValidateSocketPath(socket); err != nil {
		return nil, err
	}
	t0 := time.Now()
	cmd := exec.Command(bin, "-socket", socket)
	cmd.SysProcAttr = detachAttr()
	if stderrLog != nil {
		tightenLog(stderrLog)
		cmd.Stderr = stderrLog
	}
	if err := cmd.Start(); err != nil {
		slog.Error("daemonctl: spawn failed", "err", err)
		return nil, fmt.Errorf("spawn rex-daemon: %w", err)
	}
	pid := cmd.Process.Pid
	slog.Info("daemonctl: spawned rex-daemon", "pid", pid, "socket", socket)

	// Reap the daemon whenever it exits. Nothing else waits on it, and the
	// TUI that spawned it may outlive it by days. Buffered so the goroutine
	// never blocks once Start has returned.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	for i := 0; i < StartPollAttempts; i++ {
		if Reachable(socket) {
			elapsed := time.Since(t0)
			slog.Info("daemonctl: socket up", "socket", socket, "elapsed", elapsed)
			return &StartResult{PID: pid, Elapsed: elapsed}, nil
		}
		select {
		case waitErr := <-exited:
			err := exitedEarly(waitErr, stderrLog)
			slog.Error("daemonctl: daemon exited early", "pid", pid, "err", err)
			return nil, err
		case <-time.After(StartPollInterval):
		}
	}
	slog.Error("daemonctl: socket did not appear", "socket", socket, "timeout", StartTimeout.String())
	return nil, fmt.Errorf("daemon started but socket %s didn't appear within %s", socket, StartTimeout)
}

// exitedEarly builds Start's error for a daemon that died during startup.
func exitedEarly(waitErr error, logf *os.File) error {
	suffix := ""
	if tail := logTail(logf); tail != "" {
		suffix = "; log: " + tail
	}
	if waitErr == nil {
		return fmt.Errorf("%w (exit status 0%s)", ErrExitedEarly, suffix)
	}
	return fmt.Errorf("%w (%w%s)", ErrExitedEarly, waitErr, suffix)
}

// Bounds for logTail.
const (
	logTailBytes   = 4096
	logTailLines   = 3
	logTailLineMax = 240
)

// logTail returns the last few non-empty lines of the log behind f, joined
// with " | ", with control characters and invalid UTF-8 removed so an error
// built from it stays one printable line. f is usually the caller's
// write-only handle, so the file is reopened by name. Errors yield "".
func logTail(f *os.File) string {
	if f == nil {
		return ""
	}
	r, err := os.Open(f.Name())
	if err != nil {
		return ""
	}
	defer r.Close()
	info, err := r.Stat()
	if err != nil {
		return ""
	}
	off := max(0, info.Size()-logTailBytes)
	buf := make([]byte, info.Size()-off)
	n, _ := r.ReadAt(buf, off)
	lines := strings.Split(string(buf[:n]), "\n")
	if off > 0 {
		lines = lines[1:] // the first line may start mid-line
	}
	var kept []string
	for i := len(lines) - 1; i >= 0 && len(kept) < logTailLines; i-- {
		if l := printableLine(lines[i]); l != "" {
			kept = append(kept, l)
		}
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	return strings.Join(kept, " | ")
}

// printableLine trims s, drops control characters and invalid UTF-8, and
// caps it at logTailLineMax bytes.
func printableLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, ""))
	s = strings.TrimSpace(s)
	if len(s) > logTailLineMax {
		s = strings.ToValidUTF8(s[:logTailLineMax], "") + "…"
	}
	return s
}

// ValidateSocketPath reports whether socket fits in a unix socket address
// on this OS. sun_path holds 104 bytes on macOS and the BSDs and 108 on
// Linux and Windows, including the terminating NUL; a longer path fails
// later inside bind or connect with a bare "invalid argument".
func ValidateSocketPath(socket string) error {
	limit := maxSocketPathLen(runtime.GOOS)
	if len(socket) >= limit {
		return fmt.Errorf("socket path %s is too long: %d bytes, %s allows at most %d; set XDG_RUNTIME_DIR to a shorter directory",
			socket, len(socket), runtime.GOOS, limit-1)
	}
	return nil
}

// maxSocketPathLen is the size of sockaddr_un.sun_path on goos.
func maxSocketPathLen(goos string) int {
	switch goos {
	case "linux", "android", "windows":
		return 108
	default: // darwin, ios and the BSDs
		return 104
	}
}

// OpenLog opens LogPath for appending with private modes: the directory is
// created or tightened to 0700 and the file to 0600. Pass the result to
// Start; the caller closes it.
func OpenLog() (*os.File, error) {
	path := LogPath()
	if err := rexlog.PrivateDir(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("daemon log dir: %w", err)
	}
	f, err := rexlog.OpenPrivate(path)
	if err != nil {
		return nil, fmt.Errorf("daemon log: %w", err)
	}
	return f, nil
}

// tightenLog makes a daemon log the caller opened private: callers have
// created it 0644, and it carries session names, paths and errors. The
// standard log directory is tightened too. Best effort: a log Start cannot
// chmod is still used.
func tightenLog(f *os.File) {
	if err := f.Chmod(0o600); err != nil {
		slog.Warn("daemonctl: cannot tighten daemon log", "path", f.Name(), "err", err)
	}
	if dir := filepath.Dir(LogPath()); filepath.Dir(f.Name()) == dir {
		if err := rexlog.PrivateDir(dir); err != nil {
			slog.Warn("daemonctl: cannot tighten log dir", "dir", dir, "err", err)
		}
	}
}
