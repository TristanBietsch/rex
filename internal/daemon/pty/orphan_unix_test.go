//go:build !windows

package pty

import (
	"context"
	"errors"
	"fmt"
	"os"
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
// (so the reader has something to persist) and then idles. exec keeps the
// PID, so pidFile names the process the supervisor started. The child
// ignores SIGHUP (inherited across exec) so closing the PTY cannot stand in
// for the explicit kill under test.
func pidChildArgv(pidFile string) []string {
	return []string{"sh", "-c", `trap '' HUP; echo $$ > "$1"; echo hello; exec sleep 30`, "sh", pidFile}
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

// requireReaped fails when pid still exists, as a live process or an
// unreaped zombie. On failure it kills the stray so the test cleans up the
// process it started.
func requireReaped(t *testing.T, pid int) {
	t.Helper()
	if pid == 0 {
		return
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("child pid %d outlived Run (kill(0) = %v)", pid, err)
	}
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
		if pid := readPID(t, pidFile, 0); pid != 0 {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
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
	requireReaped(t, readPID(t, pidFile, time.Second))
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
	requireReaped(t, pid)
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
	requireReaped(t, pid)
}
