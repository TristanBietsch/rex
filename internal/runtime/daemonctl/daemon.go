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
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

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

// Start spawns rex-daemon and waits up to ~2 s for the socket to appear.
// Returns nil StartResult on failure. The caller chooses the log file (pass nil
// to discard stderr). If the daemon exits before its socket comes up, the
// error carries the last line it wrote to stderrLog (its fatal error).
func Start(socket string, stderrLog *os.File) (*StartResult, error) {
	if err := CheckSocketPath(socket); err != nil {
		return nil, err
	}
	t0 := time.Now()
	// Pass the socket Start waits for: a client run with -socket would
	// otherwise start a daemon on the default path and time out.
	cmd := exec.Command(FindBinary(), "-socket", socket)
	cmd.SysProcAttr = detachAttr()
	logStart := int64(-1)
	if stderrLog != nil {
		cmd.Stderr = stderrLog
		if info, err := stderrLog.Stat(); err == nil {
			logStart = info.Size()
		}
	}
	if err := cmd.Start(); err != nil {
		slog.Error("daemonctl: spawn failed", "err", err)
		return nil, fmt.Errorf("spawn rex-daemon: %w", err)
	}
	pid := cmd.Process.Pid
	slog.Info("daemonctl: spawned rex-daemon", "pid", pid)
	// Wait also reaps the daemon if it dies while this process (the TUI)
	// lives; otherwise this goroutine lasts as long as the daemon.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	deadline := time.NewTimer(startTimeout)
	defer deadline.Stop()
	poll := time.NewTicker(StartPollInterval)
	defer poll.Stop()
	up := func() *StartResult {
		elapsed := time.Since(t0)
		slog.Info("daemonctl: socket up", "socket", socket, "elapsed", elapsed)
		return &StartResult{PID: pid, Elapsed: elapsed}
	}
	for {
		if Reachable(socket) {
			return up(), nil
		}
		select {
		case werr := <-exited:
			return nil, startExited(pid, werr, stderrLog, logStart)
		case <-deadline.C:
			// select picks at random among ready cases, and the socket or
			// the exit may have landed since the last poll: check both once
			// more so a late success or a real exit cause is not reported
			// as a timeout.
			if Reachable(socket) {
				return up(), nil
			}
			select {
			case werr := <-exited:
				return nil, startExited(pid, werr, stderrLog, logStart)
			default:
			}
			slog.Error("daemonctl: socket did not appear", "socket", socket, "timeout", startTimeout.String())
			return nil, fmt.Errorf("rex-daemon (pid %d) is running but socket %s did not come up within %s", pid, socket, startTimeout)
		case <-poll.C:
		}
	}
}

// startExited logs and returns the error for a daemon that exited before
// its socket came up.
func startExited(pid int, werr error, stderrLog *os.File, logStart int64) error {
	err := startExitError(werr, stderrLog, logStart)
	slog.Error("daemonctl: rex-daemon exited during startup", "pid", pid, "err", err)
	return err
}

// startExitError describes a daemon that exited before its socket came up,
// with the cause from its stderr log when there is one.
func startExitError(werr error, stderrLog *os.File, logStart int64) error {
	msg := "rex-daemon exited during startup"
	if werr != nil {
		msg += " (" + werr.Error() + ")"
	}
	if stderrLog == nil || logStart < 0 {
		return errors.New(msg)
	}
	if line := lastLogLine(stderrLog.Name(), logStart); line != "" {
		return fmt.Errorf("%s: %s", msg, line)
	}
	return fmt.Errorf("%s; see %s", msg, stderrLog.Name())
}

// maxCauseRunes caps the stderr line quoted in a start error; it ends up on
// one row of the boot splash.
const maxCauseRunes = 200

// lastLogLine returns the daemon's fatal error from what was written to path
// at or after offset from (at most the last 4 KiB): the last line main
// printed ("rex-daemon: <err>", prefix dropped), else the last non-empty
// line. The result is made safe for one terminal row.
func lastLogLine(path string, from int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() <= from {
		return ""
	}
	from = max(from, info.Size()-4096)
	buf := make([]byte, info.Size()-from)
	n, _ := f.ReadAt(buf, from)
	lines := strings.Split(string(buf[:n]), "\n")
	last := ""
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if rest, ok := strings.CutPrefix(line, "rex-daemon: "); ok {
			last = rest
			break
		}
		if last == "" {
			last = line
		}
	}
	last = protocol.SingleLine(last)
	if r := []rune(last); len(r) > maxCauseRunes {
		last = string(r[:maxCauseRunes-1]) + "…"
	}
	return last
}
