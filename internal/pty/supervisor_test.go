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
