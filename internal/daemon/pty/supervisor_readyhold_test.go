package pty

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// flapAdapter replays a script, one state per Detect call, and moves a fake
// clock 500ms per call. Past the end it repeats the last state.
type flapAdapter struct {
	mu       sync.Mutex
	script   []protocol.State
	n        int
	now      time.Time
	question bool
}

func (f *flapAdapter) Detect(_ []byte, _ time.Duration) protocol.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(500 * time.Millisecond)
	st := f.script[min(f.n, len(f.script)-1)]
	f.n++
	return st
}

func (f *flapAdapter) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *flapAdapter) IsReadyForInput(_ []byte, _ time.Duration) bool { return false }

func (f *flapAdapter) WaitReason() string {
	if f.question {
		return protocol.WaitInput
	}
	return ""
}

// runFlap runs a short child under the supervisor and returns every state
// it published.
func runFlap(t *testing.T, ad *flapAdapter) []protocol.State {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the child is a Unix command")
	}
	store := state.NewStore()
	sess := &state.Session{ID: "flap1", ShortID: "flap1", ToolID: "claude", Slug: "flap",
		State: protocol.StateQueued, StartedAt: time.Now().UTC()}
	require.NoError(t, store.Add(sess))
	var (
		mu  sync.Mutex
		got []protocol.State
	)
	store.Subscribe(func(e state.Event) {
		if e.NewState != nil {
			mu.Lock()
			got = append(got, *e.NewState)
			mu.Unlock()
		}
	})
	ad.now = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sup := New(SupervisorConfig{
		StateDir: t.TempDir(), Store: store,
		Command:  []string{"sleep", "1"},
		Adapter:  ad,
		InputCh:  make(chan []byte, 1),
		IdleTick: 10 * time.Millisecond,
		Now:      ad.clock,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = sup.Run(ctx, sess)
	mu.Lock()
	defer mu.Unlock()
	return got
}

// A needs_input that flips back to working inside the hold is never
// published; one that holds 2s is published once.
func TestSupervisor_ReadyHoldSuppressesFlap(t *testing.T) {
	w, r := protocol.StateWorking, protocol.StateNeedsInput
	ad := &flapAdapter{script: []protocol.State{w, w, r, r, r, w, w, r, w, r, r, r, r, r, r}}
	got := runFlap(t, ad)
	var states []protocol.State
	for _, s := range got {
		if s == w || s == r {
			states = append(states, s)
		}
	}
	require.Equal(t, []protocol.State{w, r}, states, "one READY, after it held")
}

// A question is READY on its first tick.
func TestSupervisor_ReadyHoldSkipsQuestion(t *testing.T) {
	w, r := protocol.StateWorking, protocol.StateNeedsInput
	ad := &flapAdapter{script: []protocol.State{w, w, r, w, w}, question: true}
	got := runFlap(t, ad)
	require.Contains(t, got, r, "the question was published without a hold")
}
