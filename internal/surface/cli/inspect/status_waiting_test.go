package inspect

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// A session waiting on a tool is still a working session for rex status:
// it never counts as needing input, so the exit code (1 only while
// something needs input) does not change.
func TestStatusWaitingSessionIsWorkingNotNeedsInput(t *testing.T) {
	waiting := protocol.SessionSummary{State: protocol.StateWorking,
		WaitingOn: protocol.WaitShell, WaitingSince: time.Now().Add(-time.Hour)}
	ss := []protocol.SessionSummary{waiting}
	c := countSessions(ss)
	require.Equal(t, statusCounts{Working: 1}, c)
	require.Zero(t, c.NeedsInput, "RunStatus exits 1 only when NeedsInput > 0")
	require.Equal(t, "0 awaiting input · 1 working · 0 completed\n", emitStatus(t, "txt", false, ss))
}
