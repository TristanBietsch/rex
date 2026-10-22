package pty

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func runUntilCanceled(t *testing.T, argv []string, cause error) (*state.Store, time.Duration) {
	t.Helper()
	store := state.NewStore()
	sess := &state.Session{ID: "stop1", ShortID: "stop1", ToolID: "echo", Slug: "t",
		State: protocol.StateQueued, StartedAt: time.Now().UTC()}
	require.NoError(t, store.Add(sess))
	sup := New(SupervisorConfig{StateDir: t.TempDir(), Store: store, Command: argv,
		IdleTick: 50 * time.Millisecond, StopDrainTimeout: 2 * time.Second})

	ctx, cancel := context.WithCancelCause(context.Background())
	done := make(chan error, 1)
	go func() { done <- sup.Run(ctx, sess) }()
	time.Sleep(200 * time.Millisecond)
	start := time.Now()
	cancel(cause)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("supervisor did not return after cancel")
	}
	return store, time.Since(start)
}

func TestSupervisor_DeletedCause_NoFailedTransition(t *testing.T) {
	store, _ := runUntilCanceled(t, []string{"sleep", "30"}, ErrSessionDeleted)
	st, _ := store.CurrentState("stop1")
	require.Equal(t, protocol.StateWorking, st, "delete must not flash a failed state")
}

func TestSupervisor_KillsProcessGroup(t *testing.T) {
	// The background grandchild inherits the PTY; killing only the shell
	// would leave the reader blocked until the drain timeout.
	_, took := runUntilCanceled(t, []string{"sh", "-c", "sleep 100 & wait"}, nil)
	require.Less(t, took, 1500*time.Millisecond, "process group kill should unblock the reader promptly")
}
