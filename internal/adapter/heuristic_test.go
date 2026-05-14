package adapter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
)

func TestHeuristic_NeedsInputWhenIdleAndPromptMatches(t *testing.T) {
	h, err := NewHeuristic("^awaiting input:", 100*time.Millisecond)
	require.NoError(t, err)
	got := h.Detect([]byte("hello\nawaiting input:"), 200*time.Millisecond)
	require.Equal(t, protocol.StateNeedsInput, got)
}

func TestHeuristic_WorkingWhenNotIdle(t *testing.T) {
	h, err := NewHeuristic("^awaiting input:", 100*time.Millisecond)
	require.NoError(t, err)
	got := h.Detect([]byte("doing things..."), 10*time.Millisecond)
	require.Equal(t, protocol.StateWorking, got)
}

func TestHeuristic_WorkingWhenIdleButNoPromptMatch(t *testing.T) {
	h, err := NewHeuristic("^awaiting input:", 100*time.Millisecond)
	require.NoError(t, err)
	got := h.Detect([]byte("doing things"), 5*time.Second)
	require.Equal(t, protocol.StateWorking, got)
}

func TestNewHeuristic_RejectsBadRegex(t *testing.T) {
	_, err := NewHeuristic("[unclosed", 100*time.Millisecond)
	require.Error(t, err)
	require.Contains(t, err.Error(), "compile prompt regex")
}
