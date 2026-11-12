package loop

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The summary carries what the TUI's loops overlay shows: the minutes cap and
// the running time so far.
func TestStateSummary_CarriesMinutesCapAndElapsed(t *testing.T) {
	st := State{ID: "loop-0000000a", State: protocol.LoopRunning, Iteration: 3,
		Def:       Def{Name: "fix", Kind: "repeat", Until: Until{MaxIterations: 8, MaxMinutes: 120}},
		ElapsedNS: int64(90*time.Second + 400*time.Millisecond)}
	sum := st.Summary()
	require.Equal(t, 120, sum.MaxMinutes)
	require.EqualValues(t, 90, sum.ElapsedSeconds)

	b, err := json.Marshal(protocol.LoopSummary{ID: "x"})
	require.NoError(t, err)
	require.NotContains(t, string(b), "max_minutes", "additive and omitted when unset")
	require.NotContains(t, string(b), "elapsed_s")
}
