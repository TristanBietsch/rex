package pty

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// waitingAdapter reports a fixed tool wait.
type waitingAdapter struct {
	stubAdapter
	since time.Time
}

func (w *waitingAdapter) Waiting() (string, time.Time) { return protocol.WaitShell, w.since }

// The supervisor patches the tool wait once the session is working and
// clears it when the agent exits, before the final meta.json write.
func TestSupervisor_ReportsWaitingAndClearsAtExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the child reads its exit signal from a Unix tty")
	}
	stateDir := t.TempDir()
	store := state.NewStore()
	sess := &state.Session{ID: "wait1", ShortID: "wait1", ToolID: "claude", Slug: "wait",
		State: protocol.StateQueued, StartedAt: time.Now().UTC()}
	require.NoError(t, store.Add(sess))
	since := time.Now().UTC().Add(-time.Minute)
	ad := &waitingAdapter{since: since}
	release := make(chan struct{})
	var (
		mu   sync.Mutex
		seen []any
		once sync.Once
	)
	store.Subscribe(func(e state.Event) {
		v, ok := e.Patch["waiting_on"]
		if !ok {
			return
		}
		mu.Lock()
		seen = append(seen, v)
		mu.Unlock()
		if v == protocol.WaitShell {
			once.Do(func() { close(release) })
		}
	})
	input := make(chan []byte, 1)
	go func() {
		<-release
		input <- []byte("\n")
	}()
	sup := New(SupervisorConfig{
		StateDir: stateDir, Store: store,
		Command:  []string{"sh", "-c", "read x"},
		Adapter:  ad,
		InputCh:  input,
		IdleTick: 20 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, sup.Run(ctx, sess))

	mu.Lock()
	require.Equal(t, []any{protocol.WaitShell, nil}, seen, "set once, cleared at exit")
	mu.Unlock()
	sum := sess.Summary()
	require.Empty(t, sum.WaitingOn)
	meta, err := os.ReadFile(filepath.Join(stateDir, "sessions", sess.ID, "meta.json"))
	require.NoError(t, err)
	require.NotContains(t, string(meta), "waiting_on", "the final meta.json keeps no wait")
}
