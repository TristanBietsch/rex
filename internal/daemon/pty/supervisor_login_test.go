package pty

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/adapter"
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// A signed-out agent's session reads needs_input with the fixed notice as its
// last line, not the agent's own screen text.
func TestSupervisor_LoginScreenShowsNotice(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh and a Unix tty")
	}
	reg, err := registry.Load("")
	require.NoError(t, err)
	tool, ok := reg.Find("cursor")
	require.True(t, ok)
	ad, err := adapter.For(tool, "")
	require.NoError(t, err)

	store := state.NewStore()
	sess := &state.Session{ID: "login1", ShortID: "login1", ToolID: "cursor", Slug: "login",
		State: protocol.StateQueued, StartedAt: time.Now().UTC()}
	require.NoError(t, store.Add(sess))
	sup := New(SupervisorConfig{
		StateDir: t.TempDir(), Store: store,
		Command:  []string{"sh", "-c", `printf 'Press any key to log in...'; sleep 30`},
		Adapter:  ad,
		IdleTick: 20 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sup.Run(ctx, sess) }()

	want := "cursor: not signed in — run `cursor-agent login`"
	require.Eventually(t, func() bool {
		st, _ := store.CurrentState("login1")
		for _, s := range store.Snapshot() {
			if s.ID == "login1" && s.LastLine == want {
				return st == protocol.StateNeedsInput
			}
		}
		return false
	}, 10*time.Second, 50*time.Millisecond)
	cancel()
	<-done
}
