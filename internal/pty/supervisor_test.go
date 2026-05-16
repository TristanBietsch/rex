package pty

import (
	"context"
	"strings"
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
