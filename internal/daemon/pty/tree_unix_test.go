//go:build !windows

package pty

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// bgPIDRe finds the pid a test script prints for its background job.
var bgPIDRe = regexp.MustCompile(`bg=(\d+)`)

// requireGone fails unless pid exits within a second. A leftover process is
// killed so a failing run doesn't leak it.
func requireGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("background job %d outlived the session", pid)
}

// bgPID reads the background job's pid from the session transcript.
func bgPID(t *testing.T, stateDir, id string) int {
	t.Helper()
	tail, err := state.TranscriptTail(stateDir, id, 4096)
	require.NoError(t, err)
	m := bgPIDRe.FindSubmatch(tail)
	require.NotNil(t, m, "no bg pid in transcript %q", tail)
	pid, err := strconv.Atoi(string(m[1]))
	require.NoError(t, err)
	return pid
}

// orphanScript starts a background job that ignores SIGHUP (so the session
// leader's exit doesn't end it) and doesn't hold the PTY (so the reader still
// sees EOF), like a detached MCP server.
const orphanScript = `trap '' HUP; sleep 30 </dev/null >/dev/null 2>&1 & echo "bg=$!"`

// An agent that exits on its own must take its background jobs with it.
func TestSupervisor_NaturalExitKillsProcessGroup(t *testing.T) {
	stateDir := t.TempDir()
	store := state.NewStore()
	sess := &state.Session{ID: "orph1", ShortID: "orph1", ToolID: "echo", Slug: "t",
		State: protocol.StateQueued, StartedAt: time.Now().UTC()}
	require.NoError(t, store.Add(sess))
	sup := New(SupervisorConfig{StateDir: stateDir, Store: store,
		Command: []string{"sh", "-c", orphanScript}, IdleTick: 10 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, sup.Run(ctx, sess))
	requireGone(t, bgPID(t, stateDir, "orph1"))
}

// A setup failure after the child started must neither leave the agent
// running unsupervised nor leave the session stuck in working.
func TestSupervisor_SetupFailureStopsChild(t *testing.T) {
	stateDir := t.TempDir()
	// A directory in place of transcript.log makes OpenTranscript fail.
	require.NoError(t, os.MkdirAll(filepath.Join(stateDir, "sessions", "setup1", "transcript.log"), 0o700))
	pidFile := filepath.Join(t.TempDir(), "pid")
	store := state.NewStore()
	sess := &state.Session{ID: "setup1", ShortID: "setup1", ToolID: "echo", Slug: "t",
		State: protocol.StateQueued, StartedAt: time.Now().UTC()}
	require.NoError(t, store.Add(sess))
	// Ignoring SIGHUP keeps the child alive past the PTY close in Run.
	sup := New(SupervisorConfig{StateDir: stateDir, Store: store,
		Command: []string{"sh", "-c", `trap '' HUP; echo "$$" > "$0"; exec sleep 30`, pidFile}})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.Error(t, sup.Run(ctx, sess))

	st, _ := store.CurrentState("setup1")
	require.Equal(t, protocol.StateFailed, st)
	m, err := state.LoadMeta(stateDir, "setup1")
	require.NoError(t, err)
	require.Equal(t, protocol.StateFailed, m.State)

	// The kill may land before the child records its pid; if it got that
	// far, it must be gone.
	time.Sleep(200 * time.Millisecond)
	if b, err := os.ReadFile(pidFile); err == nil {
		pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
		require.NoError(t, err)
		requireGone(t, pid)
	}
}

// Stopping a session (daemon shutdown, Delete) kills background jobs too, not
// just the direct child.
func TestSupervisor_CancelKillsBackgroundJobs(t *testing.T) {
	stateDir := t.TempDir()
	store := state.NewStore()
	sess := &state.Session{ID: "orph2", ShortID: "orph2", ToolID: "echo", Slug: "t",
		State: protocol.StateQueued, StartedAt: time.Now().UTC()}
	require.NoError(t, store.Add(sess))
	sup := New(SupervisorConfig{StateDir: stateDir, Store: store,
		Command:  []string{"sh", "-c", orphanScript + "; wait"},
		IdleTick: 10 * time.Millisecond, StopDrainTimeout: 2 * time.Second})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sup.Run(ctx, sess) }()
	require.Eventually(t, func() bool {
		tail, _ := state.TranscriptTail(stateDir, "orph2", 4096)
		return bgPIDRe.Match(tail)
	}, 2*time.Second, 20*time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("supervisor did not return after cancel")
	}
	requireGone(t, bgPID(t, stateDir, "orph2"))
}
