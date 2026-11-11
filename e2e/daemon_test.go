package e2e

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rogpeppe/go-internal/testscript"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

const (
	// socketWait bounds how long daemon-start waits for the socket to accept.
	socketWait = 10 * time.Second
	// stopWait bounds how long daemon-stop and cleanup wait after SIGTERM
	// before falling back to SIGKILL.
	stopWait = 10 * time.Second
	// dialTimeout keeps each socket probe short so a wedged daemon cannot
	// stall the harness.
	dialTimeout = 200 * time.Millisecond
)

// scriptDaemon is the one rex-daemon a script started with daemon-start.
type scriptDaemon struct {
	cmd    *exec.Cmd
	done   chan struct{} // closed once cmd.Wait returns
	err    error         // cmd.Wait result, valid after done is closed
	stderr string        // path of the file capturing the daemon's output
}

// harness returns the per-script state Setup registered.
func harness(ts *testscript.TestScript) *scriptHarness {
	h, _ := ts.Value(harnessKey{}).(*scriptHarness)
	if h == nil {
		ts.Fatalf("e2e: script harness missing; was Setup skipped?")
	}
	return h
}

// cmdDaemonStart execs the script's rex-daemon on its isolated socket and
// state dir and waits until the socket accepts connections.
//
//	daemon-start [extra rex-daemon flags...]
//
// It refuses to start with the summarizer enabled, because the daemon would
// then call whatever Ollama listens on localhost.
func cmdDaemonStart(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("unsupported: ! daemon-start")
	}
	h := harness(ts)
	if h.daemon != nil {
		ts.Fatalf("daemon-start: daemon already running (pid %d)", h.daemon.cmd.Process.Pid)
	}
	home := ts.Getenv("HOME")
	cfg := settings.NewStore()
	ts.Check(cfg.Load(filepath.Join(home, ".config", "rex", "config.yaml")))
	if on, _ := cfg.Get("summary_enabled").(bool); on {
		ts.Fatalf("daemon-start: summary_enabled is on; add home/.config/rex/config.yaml with `summary_enabled: false` to the script")
	}

	stderrPath := ts.MkAbs("daemon.stderr")
	stderr, err := os.Create(stderrPath)
	ts.Check(err)
	defer stderr.Close()

	flags := append([]string{
		"-socket", h.socket,
		"-state-dir", filepath.Join(home, ".local", "share", "rex"),
		"-tools", filepath.Join(home, ".config", "rex", "tools.yaml"),
	}, args...)
	cmd, err := startRetryingTextBusy(func() *exec.Cmd {
		c := exec.Command(h.daemonBin, flags...)
		c.Dir = ts.MkAbs(".")
		c.Env = scriptEnv(ts)
		c.Stdout = stderr
		c.Stderr = stderr
		return c
	})
	ts.Check(err)

	d := &scriptDaemon{cmd: cmd, done: make(chan struct{}), stderr: stderrPath}
	go func() {
		d.err = cmd.Wait()
		close(d.done)
	}()
	// From here on Setup's cleanup stops d if the script fails or never
	// calls daemon-stop. It only ever signals this PID.
	h.daemon = d

	deadline := time.Now().Add(socketWait)
	for !dialable(h.socket) {
		select {
		case <-d.done:
			ts.Fatalf("daemon-start: rex-daemon exited early (%v):\n%s", d.err, ts.ReadFile(stderrPath))
		default:
		}
		if time.Now().After(deadline) {
			ts.Fatalf("daemon-start: socket %s not dialable after %s:\n%s", h.socket, socketWait, ts.ReadFile(stderrPath))
		}
		time.Sleep(20 * time.Millisecond)
	}
	ts.Logf("daemon-start: rex-daemon pid %d on %s", cmd.Process.Pid, h.socket)
}

// cmdDaemonStop sends SIGTERM to the daemon daemon-start launched and waits
// for it to exit cleanly.
//
//	daemon-stop
func cmdDaemonStop(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 0 {
		ts.Fatalf("usage: daemon-stop")
	}
	h := harness(ts)
	d := h.daemon
	if d == nil {
		ts.Fatalf("daemon-stop: no daemon started")
	}
	h.daemon = nil
	ts.Check(d.cmd.Process.Signal(syscall.SIGTERM))
	select {
	case <-d.done:
	case <-time.After(stopWait):
		_ = d.cmd.Process.Kill()
		<-d.done
		ts.Fatalf("daemon-stop: rex-daemon ignored SIGTERM for %s:\n%s", stopWait, ts.ReadFile(d.stderr))
	}
	if d.err != nil {
		ts.Fatalf("daemon-stop: rex-daemon exited with %v:\n%s", d.err, ts.ReadFile(d.stderr))
	}
}

// stopDaemon terminates d if it is still running: SIGTERM, then SIGKILL
// after stopWait. It is a no-op for nil or already-exited daemons.
func stopDaemon(d *scriptDaemon) {
	if d == nil {
		return
	}
	select {
	case <-d.done:
		return
	default:
	}
	_ = d.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-d.done:
	case <-time.After(stopWait):
		_ = d.cmd.Process.Kill()
		<-d.done
	}
}

// daemonPIDs lists live processes whose command line starts with daemonBin.
// daemonBin sits under one script's $WORK, so the match can never include
// the user's daemons or another script's. Both the literal and the
// symlink-resolved path are matched, because macOS may report /var/... as
// /private/var/....
func daemonPIDs(daemonBin string) []int {
	alts := []string{regexp.QuoteMeta(daemonBin)}
	if real, err := filepath.EvalSymlinks(daemonBin); err == nil && real != daemonBin {
		alts = append(alts, regexp.QuoteMeta(real))
	}
	pattern := "^(" + strings.Join(alts, "|") + ")( |$)"
	out, err := exec.Command("pgrep", "-f", pattern).Output()
	if err != nil {
		return nil // pgrep exits 1 when nothing matches
	}
	var pids []int
	for _, f := range strings.Fields(string(out)) {
		if pid, err := strconv.Atoi(f); err == nil {
			pids = append(pids, pid)
		}
	}
	return pids
}

// reapStrayDaemons stops every process still running daemonBin, such as a
// daemon rex auto-started with `rex daemon start`: SIGTERM, then SIGKILL for
// any that outlive stopWait. Each signal goes only to PIDs that matched
// daemonBin moments before, so a recycled PID is never hit. It returns the
// PIDs it found.
func reapStrayDaemons(daemonBin string) []int {
	found := daemonPIDs(daemonBin)
	signalAll(found, syscall.SIGTERM)
	deadline := time.Now().Add(stopWait)
	for len(daemonPIDs(daemonBin)) > 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	signalAll(daemonPIDs(daemonBin), syscall.SIGKILL)
	return found
}

// signalAll sends sig to each pid, ignoring processes that already exited.
func signalAll(pids []int, sig syscall.Signal) {
	for _, pid := range pids {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Signal(sig)
		}
	}
}

// dialable reports whether something accepts connections on sock.
func dialable(sock string) bool {
	conn, err := net.DialTimeout("unix", sock, dialTimeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// shutdownViaSocket asks the daemon on sock to exit with the Shutdown intent
// and waits for the socket to stop answering. It is the fallback for a
// daemon the harness can find neither by PID nor by path.
func shutdownViaSocket(sock string) {
	conn, err := net.DialTimeout("unix", sock, dialTimeout)
	if err != nil {
		return
	}
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	w := protocol.NewWriter(conn)
	_ = w.WriteIntent(protocol.IntentHello, "h", protocol.Hello{ClientVersion: "rex-e2e"})
	_ = w.WriteIntent(protocol.IntentShutdown, "", struct{}{})
	// Close before waiting: the daemon does not finish shutting down while
	// a client is still connected.
	_ = conn.Close()
	deadline := time.Now().Add(stopWait)
	for dialable(sock) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
}

// startRetryingTextBusy starts the command newCmd builds, retrying while
// exec fails with ETXTBSY. Each script runs a private copy of rex-daemon
// (copyBinary), and on Linux a process forked by a parallel script while
// the copy was still open for writing holds that write handle until it
// execs; exec of the copy fails with "text file busy" meanwhile
// (golang/go#22315). It clears within milliseconds. A Cmd cannot be
// started twice, so each attempt builds a fresh one.
func startRetryingTextBusy(newCmd func() *exec.Cmd) (*exec.Cmd, error) {
	delay := 10 * time.Millisecond
	for attempt := 0; ; attempt++ {
		cmd := newCmd()
		err := cmd.Start()
		if err == nil || !errors.Is(err, syscall.ETXTBSY) || attempt == 8 {
			return cmd, err
		}
		time.Sleep(delay)
		delay *= 2
	}
}
