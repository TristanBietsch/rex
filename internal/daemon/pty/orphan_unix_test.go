//go:build !windows

package pty

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// pidChildArgv runs a child that records its PID in pidFile, prints a line
// (so the reader has something to persist) and then idles. The shell idles
// itself rather than exec'ing sleep, so pidFile names the process the
// supervisor started and that process's command line keeps naming pidFile,
// which is how killIfOwnChild recognizes it. The child ignores SIGHUP (its
// sleeps inherit that) so closing the PTY cannot stand in for the explicit
// kill under test.
func pidChildArgv(pidFile string) []string {
	return []string{"sh", "-c", `trap '' HUP; echo $$ > "$1"; echo hello; while :; do sleep 1; done`, "sh", pidFile}
}

// chattyPIDChildArgv is pidChildArgv for a child that never stops printing,
// like an agent streaming output: killing it only completes if something
// keeps draining the PTY.
func chattyPIDChildArgv(pidFile string) []string {
	return []string{"sh", "-c", `trap '' HUP; echo $$ > "$1"; while :; do echo hello; done`, "sh", pidFile}
}

// readPID waits up to wait for pidFile and returns its PID, or 0 when the
// child never wrote it (it was killed first).
func readPID(t *testing.T, pidFile string, wait time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(wait)
	for {
		b, err := os.ReadFile(pidFile)
		if err == nil && strings.HasSuffix(string(b), "\n") {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(b)))
			require.NoError(t, convErr)
			return pid
		}
		if time.Now().After(deadline) {
			return 0
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// requireReaped fails when pid, the child named by pidFile, still exists
// as a child of this test, live or an unreaped zombie. A PID that now
// belongs to some other process means the child was reaped and the PID
// recycled. On failure it kills the stray, but only through
// killIfOwnChild, so a recycled PID is never signaled.
func requireReaped(t *testing.T, pid int, pidFile string) {
	t.Helper()
	if pid == 0 || errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		return
	}
	ppid, args, err := psProcess(pid)
	if err != nil {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return // exited between the two checks
		}
		t.Fatalf("child pid %d: cannot identify it: %v", pid, err)
	}
	if ppid != os.Getpid() {
		return // reaped; the PID now belongs to an unrelated process
	}
	killIfOwnChild(pid, pidFile)
	t.Fatalf("child pid %d outlived Run (%s)", pid, args)
}

// psProcess returns pid's parent PID and command line as ps reports them.
// -ww keeps ps from truncating the command line; ps exits non-zero when
// pid does not exist.
func psProcess(pid int) (ppid int, args string, err error) {
	out, err := exec.Command("ps", "-ww", "-o", "ppid=", "-o", "args=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, "", fmt.Errorf("ps -p %d: %w", pid, err)
	}
	field, args, _ := strings.Cut(strings.TrimSpace(string(out)), " ")
	ppid, err = strconv.Atoi(field)
	if err != nil {
		return 0, "", fmt.Errorf("ps -p %d: parse ppid %q: %w", pid, field, err)
	}
	return ppid, strings.TrimSpace(args), nil
}

// killIfOwnChild SIGKILLs pid only while it is still this test's child and
// its command line names pidFile, and reports whether it did. pidFile sits
// in the test's own t.TempDir, so no other process names it. A PID read
// back from a file may have been recycled by an unrelated process (an e2e
// rex-daemon, the user's shell) once the child exited; that process must
// never be signaled.
func killIfOwnChild(pid int, pidFile string) bool {
	if pid <= 0 {
		return false
	}
	ppid, args, err := psProcess(pid)
	if err != nil || ppid != os.Getpid() || !strings.Contains(args, pidFile) {
		return false
	}
	return syscall.Kill(pid, syscall.SIGKILL) == nil
}

// orphanStopTimeout is the StopDrainTimeout these tests configure. Run must
// return well inside it: hitting it means the reap timed out and the child
// was only freed later, when Run's deferred PTY close ran.
const orphanStopTimeout = 3 * time.Second

// runBounded runs sup.Run and fails the test if it does not return in time.
// ctx stays live until cleanup: canceling it as Run returns would let
// exec.CommandContext kill the child and mask a missing kill in Run.
func runBounded(t *testing.T, sup *Supervisor, sess *state.Session, pidFile string) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- sup.Run(ctx, sess) }()
	select {
	case err := <-done:
		require.Less(t, time.Since(start), orphanStopTimeout/2, "Run hit the reap timeout")
		return err
	case <-time.After(2 * orphanStopTimeout):
		killIfOwnChild(readPID(t, pidFile, 0), pidFile)
		<-done
		t.Fatal("Run did not return after the failure; the child was left running")
		return nil
	}
}

func newOrphanTestSession(t *testing.T, store *state.Store, add bool) *state.Session {
	t.Helper()
	sess := &state.Session{ID: "orph1", ShortID: "orph1", ToolID: "echo", Slug: "t",
		State: protocol.StateQueued, StartedAt: time.Now().UTC()}
	if add {
		require.NoError(t, store.Add(sess))
	}
	return sess
}

// failingTranscript fails every write the way a full disk does.
type failingTranscript struct{}

func (failingTranscript) Write([]byte) error {
	return fmt.Errorf("write transcript: %w", syscall.ENOSPC)
}

func (failingTranscript) Close() error { return nil }

// P0-05: a transcript write error ends the reader. The child must be killed
// and reaped, not left running while Run blocks in Wait.
func TestSupervisor_TranscriptWriteErrorKillsChild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	store := state.NewStore()
	sess := newOrphanTestSession(t, store, true)
	sup := New(SupervisorConfig{StateDir: t.TempDir(), Store: store, Command: chattyPIDChildArgv(pidFile),
		IdleTick: 10 * time.Millisecond, StopDrainTimeout: orphanStopTimeout})
	sup.openTranscript = func(string, string) (transcriptWriter, error) { return failingTranscript{}, nil }

	err := runBounded(t, sup, sess, pidFile)
	require.ErrorIs(t, err, syscall.ENOSPC)
	requireReaped(t, readPID(t, pidFile, time.Second), pidFile)
	st, _ := store.CurrentState(sess.ID)
	require.Equal(t, protocol.StateFailed, st)
}

// P0-05: OpenTranscript failing after the console started used to return
// without touching the child.
func TestSupervisor_OpenTranscriptErrorKillsChild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	store := state.NewStore()
	sess := newOrphanTestSession(t, store, true)
	sup := New(SupervisorConfig{StateDir: t.TempDir(), Store: store, Command: pidChildArgv(pidFile),
		IdleTick: 10 * time.Millisecond, StopDrainTimeout: orphanStopTimeout})
	openErr := errors.New("injected open failure")
	var pid int
	sup.openTranscript = func(string, string) (transcriptWriter, error) {
		// Fail only once the child is provably running.
		pid = readPID(t, pidFile, 2*time.Second)
		return nil, openErr
	}

	err := runBounded(t, sup, sess, pidFile)
	require.ErrorIs(t, err, openErr)
	require.NotZero(t, pid, "child never wrote its pid")
	requireReaped(t, pid, pidFile)
}

// P0-05: a Transition error (session not in the store) after the console
// started used to return without touching the child.
func TestSupervisor_TransitionErrorKillsChild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")
	store := state.NewStore()
	sess := newOrphanTestSession(t, store, false)
	var pid int
	sup := New(SupervisorConfig{StateDir: t.TempDir(), Store: store, Command: pidChildArgv(pidFile),
		IdleTick: 10 * time.Millisecond, StopDrainTimeout: orphanStopTimeout,
		// Run registers the resize hook before the Transition; hold it until
		// the child has trapped SIGHUP and recorded its pid.
		RegisterResize: func(func(cols, rows uint16) error) {
			pid = readPID(t, pidFile, 2*time.Second)
		},
	})

	err := runBounded(t, sup, sess, pidFile)
	require.Error(t, err)
	require.NotZero(t, pid, "child never wrote its pid")
	requireReaped(t, pid, pidFile)
}

// The cleanup in requireReaped and runBounded kills a PID read back from a
// pid file. By then the child may be gone and its PID recycled, so only a
// process that is still this test's child and names pidFile may be killed.
func TestKillIfOwnChild_SparesOtherProcesses(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "pid")

	// A child of this test that pidFile does not name, standing in for a
	// process that reused the PID.
	other := exec.Command("sleep", "30")
	require.NoError(t, other.Start())
	t.Cleanup(func() {
		_ = other.Process.Kill()
		_ = other.Wait()
	})
	require.False(t, killIfOwnChild(other.Process.Pid, pidFile))
	require.NoError(t, other.Process.Signal(syscall.Signal(0)), "killed a process pidFile does not name")

	// The child pidFile names is killed.
	argv := pidChildArgv(pidFile)
	own := exec.Command(argv[0], argv[1:]...)
	require.NoError(t, own.Start())
	var waitErr error
	waited := make(chan struct{})
	go func() {
		waitErr = own.Wait()
		close(waited)
	}()
	t.Cleanup(func() {
		_ = own.Process.Kill()
		<-waited
	})
	pid := readPID(t, pidFile, 5*time.Second)
	require.Equal(t, own.Process.Pid, pid)
	require.True(t, killIfOwnChild(pid, pidFile))
	select {
	case <-waited:
		require.ErrorContains(t, waitErr, "signal: killed")
	case <-time.After(5 * time.Second):
		t.Fatal("own child survived killIfOwnChild")
	}
}
