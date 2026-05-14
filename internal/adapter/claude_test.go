package adapter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
)

func TestClaudeStructured_AssistantMessage(t *testing.T) {
	a := NewClaudeStructured()
	line := []byte(`{"type":"assistant","message":{"content":"hello"}}` + "\n")
	got := a.Detect(line, 100*time.Millisecond)
	require.Equal(t, protocol.StateWorking, got)
}

func TestClaudeStructured_ResultMessage(t *testing.T) {
	a := NewClaudeStructured()
	line := []byte(`{"type":"result","subtype":"success"}` + "\n")
	got := a.Detect(line, 100*time.Millisecond)
	require.Equal(t, protocol.StateDone, got)
}

func TestClaudeStructured_IdleFallback(t *testing.T) {
	a := NewClaudeStructured()
	// Seed with an assistant message so last = working.
	a.Detect([]byte(`{"type":"assistant"}`+"\n"), 100*time.Millisecond)
	// Force lastSeen back so we're "idle".
	a.lastSeen = time.Now().Add(-6 * time.Second)
	got := a.Detect(nil, time.Second)
	require.Equal(t, protocol.StateNeedsInput, got)
}

func TestClaudeStructured_PartialLineBuffered(t *testing.T) {
	a := NewClaudeStructured()
	// Send half a line first — should not panic, state stays working.
	got := a.Detect([]byte(`{"type":"result"`), 10*time.Millisecond)
	require.Equal(t, protocol.StateWorking, got)
	// Send the rest with a newline.
	got = a.Detect([]byte(`,"subtype":"success"}`+"\n"), 10*time.Millisecond)
	require.Equal(t, protocol.StateDone, got)
}
