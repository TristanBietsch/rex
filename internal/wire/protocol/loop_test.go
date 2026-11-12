package protocol

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLoopSummary_RoundTrip(t *testing.T) {
	exit := 1
	l := LoopSummary{
		ID: "loop-0a1b2c3d", Name: "fix", Kind: "repeat", State: LoopPaused, PauseReason: PauseNeedsInput,
		SessionID: "s1", CWD: "/work", Iteration: 3, MaxIterations: 8, LastCheckExit: &exit,
		StartedAt: time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 11, 11, 10, 5, 0, 0, time.UTC),
		Notice: "n",
	}
	b, err := json.Marshal(l)
	require.NoError(t, err)
	var back LoopSummary
	require.NoError(t, json.Unmarshal(b, &back))
	require.Equal(t, l, back)
}

// The loop fields are additive: a session that is not in a loop, and a
// snapshot with no loops, encode exactly as before.
func TestLoopFields_AreAdditive(t *testing.T) {
	b, err := json.Marshal(SessionSummary{ID: "a"})
	require.NoError(t, err)
	require.NotContains(t, string(b), "loop")
	b, err = json.Marshal(Snapshot{Sessions: []SessionSummary{}})
	require.NoError(t, err)
	require.NotContains(t, string(b), "loops")

	var s SessionSummary
	require.NoError(t, json.Unmarshal([]byte(`{"id":"a","loop_id":"loop-00000001","loop_iter":2}`), &s))
	require.Equal(t, "loop-00000001", s.LoopID)
	require.Equal(t, 2, s.LoopIter)
}

func TestLoopState_Terminal(t *testing.T) {
	for _, s := range []LoopState{LoopDone, LoopCapped, LoopStopped, LoopFailed} {
		require.True(t, s.Terminal(), s)
	}
	for _, s := range []LoopState{LoopRunning, LoopPaused, ""} {
		require.False(t, s.Terminal(), s)
	}
}

func TestValidLoopID(t *testing.T) {
	require.True(t, ValidLoopID("loop-0a1b2c3d"))
	for _, id := range []string{"", "loop-", "loop-0A1B2C3D", "loop-0a1b2c3", "loop-0a1b2c3d0", "../loop-0a1b2c3d", "loop-0a1b2c3d/../x", "x-0a1b2c3d"} {
		require.False(t, ValidLoopID(id), id)
	}
}
