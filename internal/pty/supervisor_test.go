package pty

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/state"
)

func TestSupervisor_RunEchoToCompletion(t *testing.T) {
	stateDir := t.TempDir()
	store := state.NewStore()
	sess := &state.Session{
		ID:        "id1",
		ShortID:   "id1",
		ToolID:    "echo",
		ModelID:   "short",
		Slug:      "test",
		State:     protocol.StateQueued,
		StartedAt: time.Now().UTC(),
	}
	require.NoError(t, store.Add(sess))

	output := make(chan []byte, 32)
	sup := New(SupervisorConfig{
		StateDir:   stateDir,
		Store:      store,
		Command:    []string{"bash", "-c", "echo hello; echo done"},
		Adapter:    nil, // unused: we'll mark done on exit
		OutputSink: func(b []byte) { output <- b },
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := sup.Run(ctx, sess)
	require.NoError(t, err)

	got, _ := store.Get("id1")
	require.Equal(t, protocol.StateDone, got.State)

	// Should have collected something on the output channel.
	close(output)
	var all strings.Builder
	for chunk := range output {
		all.Write(chunk)
	}
	require.Contains(t, all.String(), "hello")
}

func TestLastNonEmptyLine_SkipsSpinnerLines(t *testing.T) {
	// Ollama renders a Braille-spinner frame as the most recent line while
	// loading; we want the prior real line instead.
	input := []byte("loaded model\n⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏\n")
	got := lastNonEmptyLine(input)
	require.Equal(t, "loaded model", got)
}

func TestLastNonEmptyLine_FallsBackWhenAllSpinner(t *testing.T) {
	input := []byte("⠋⠙⠹⠸⠼\n⠦⠧⠇⠏⠹\n")
	got := lastNonEmptyLine(input)
	// Falls back to the most recent line so we never return empty.
	require.NotEmpty(t, got)
}

func TestLastNonEmptyLine_KeepsRealText(t *testing.T) {
	input := []byte("hi\n>>> Send a message (/? for help)")
	got := lastNonEmptyLine(input)
	require.Equal(t, ">>> Send a message (/? for help)", got)
}

// stubAdapter returns a programmable sequence of states on successive Detect calls.
// After the sequence is exhausted, it keeps returning the final element.
type stubAdapter struct {
	mu       sync.Mutex
	sequence []protocol.State
	idx      int
}

func (s *stubAdapter) Detect(window []byte, idle time.Duration) protocol.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sequence) == 0 {
		return protocol.StateWorking
	}
	if s.idx >= len(s.sequence) {
		return s.sequence[len(s.sequence)-1]
	}
	out := s.sequence[s.idx]
	s.idx++
	return out
}

func TestSupervisor_NeedsInputToWorkingRegression(t *testing.T) {
	stateDir := t.TempDir()
	store := state.NewStore()
	sess := &state.Session{
		ID: "id1", ShortID: "id1", ToolID: "echo", Slug: "test",
		State: protocol.StateQueued, StartedAt: time.Now().UTC(),
	}
	require.NoError(t, store.Add(sess))

	var (
		mu          sync.Mutex
		transitions []protocol.State
	)
	store.Subscribe(func(e state.Event) {
		if e.NewState != nil {
			mu.Lock()
			transitions = append(transitions, *e.NewState)
			mu.Unlock()
		}
	})

	stub := &stubAdapter{sequence: []protocol.State{
		protocol.StateWorking,    // same as initial — no transition expected
		protocol.StateNeedsInput, // working -> needs_input
		protocol.StateNeedsInput, // no transition (deduped)
		protocol.StateWorking,    // the regression — needs_input -> working
		protocol.StateWorking,
	}}

	sup := New(SupervisorConfig{
		StateDir: stateDir, Store: store,
		Command:  []string{"sleep", "0.5"},
		Adapter:  stub,
		IdleTick: 10 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = sup.Run(ctx, sess)

	mu.Lock()
	defer mu.Unlock()
	sawNeeds, sawWorkingAfterNeeds := false, false
	for _, st := range transitions {
		if st == protocol.StateNeedsInput {
			sawNeeds = true
			continue
		}
		if sawNeeds && st == protocol.StateWorking {
			sawWorkingAfterNeeds = true
		}
	}
	require.True(t, sawNeeds, "expected needs_input transition; got %v", transitions)
	require.True(t, sawWorkingAfterNeeds, "expected needs_input -> working regression; got %v", transitions)
}

func TestSupervisor_CompleteSignalEndsCleanlyWithStateDone(t *testing.T) {
	stateDir := t.TempDir()
	store := state.NewStore()
	sess := &state.Session{
		ID: "id1", ShortID: "id1", ToolID: "echo", Slug: "test",
		State: protocol.StateQueued, StartedAt: time.Now().UTC(),
	}
	require.NoError(t, store.Add(sess))

	completeCh := make(chan struct{}, 1)
	sup := New(SupervisorConfig{
		StateDir:   stateDir,
		Store:      store,
		Command:    []string{"sleep", "30"}, // long enough that we know we triggered it
		CompleteCh: completeCh,
		IdleTick:   50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- sup.Run(ctx, sess) }()

	// Let it spawn and reach StateWorking.
	time.Sleep(100 * time.Millisecond)
	completeCh <- struct{}{}

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not return after complete signal")
	}

	got, _ := store.Get("id1")
	require.Equal(t, protocol.StateDone, got.State)
}

func TestSupervisor_CtxCancelStillProducesFailed(t *testing.T) {
	// Regression guard: existing ctx-cancel behavior must remain StateFailed.
	stateDir := t.TempDir()
	store := state.NewStore()
	sess := &state.Session{
		ID: "id2", ShortID: "id2", ToolID: "echo", Slug: "test",
		State: protocol.StateQueued, StartedAt: time.Now().UTC(),
	}
	require.NoError(t, store.Add(sess))

	sup := New(SupervisorConfig{
		StateDir: stateDir, Store: store,
		Command:  []string{"sleep", "30"},
		IdleTick: 50 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sup.Run(ctx, sess) }()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not return after ctx cancel")
	}

	got, _ := store.Get("id2")
	require.Equal(t, protocol.StateFailed, got.State)
}

// TestSummaryTriggerFunc verifies the standalone trigger predicate used inside
// the supervisor's ticker branch. Kept as a pure function to make the timing
// behavior testable without spawning a child process.
func TestSummaryTriggerFunc(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name        string
		dirty       bool
		idle        time.Duration
		sinceSubmit time.Duration
		want        bool
	}{
		{"clean: never fire", false, 1 * time.Second, 1 * time.Second, false},
		{"dirty + idle below threshold + ceiling cold", true, 200 * time.Millisecond, 1 * time.Second, false},
		{"dirty + idle reached", true, 500 * time.Millisecond, 1 * time.Second, true},
		{"dirty + ceiling reached even though busy", true, 50 * time.Millisecond, 16 * time.Second, true},
		{"dirty but neither idle nor ceiling", true, 200 * time.Millisecond, 5 * time.Second, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lastSubmittedAt := now.Add(-tc.sinceSubmit)
			lastChunk := now.Add(-tc.idle)
			got := shouldEmitSummary(tc.dirty, lastChunk, lastSubmittedAt, now)
			if got != tc.want {
				t.Fatalf("shouldEmitSummary(dirty=%v idle=%v sinceSubmit=%v) = %v want %v",
					tc.dirty, tc.idle, tc.sinceSubmit, got, tc.want)
			}
		})
	}
}
