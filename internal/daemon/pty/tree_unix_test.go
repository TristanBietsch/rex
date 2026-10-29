//go:build !windows

package pty

import (
	"context"
	"errors"
	"regexp"
	"strconv"
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
