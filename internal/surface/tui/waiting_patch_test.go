package tui

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// patchOverWire sends a patch through JSON as the daemon does, so times
// arrive as strings and nulls as nil.
func patchOverWire(t *testing.T, patch map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(patch)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func TestApplyPatch_Waiting(t *testing.T) {
	at := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	s := protocol.SessionSummary{State: protocol.StateWorking}

	applyPatch(&s, patchOverWire(t, map[string]any{"waiting_on": "shell", "waiting_since": at}))
	require.Equal(t, protocol.WaitShell, s.WaitingOn)
	require.True(t, at.Equal(s.WaitingSince))

	// A state patch alone leaves the wait untouched.
	applyPatch(&s, patchOverWire(t, map[string]any{"last_line": "x"}))
	require.Equal(t, protocol.WaitShell, s.WaitingOn)

	applyPatch(&s, patchOverWire(t, map[string]any{"waiting_on": nil, "waiting_since": nil}))
	require.Empty(t, s.WaitingOn)
	require.True(t, s.WaitingSince.IsZero())

	applyPatch(&s, patchOverWire(t, map[string]any{"waiting_on": "web", "waiting_since": at}))
	applyPatch(&s, patchOverWire(t, map[string]any{"waiting_on": ""}))
	require.Empty(t, s.WaitingOn, `"" clears`)

	// A kind from a newer daemon reads as not waiting.
	applyPatch(&s, patchOverWire(t, map[string]any{"waiting_on": "teleport", "waiting_since": at}))
	require.Empty(t, s.WaitingOn)
}
