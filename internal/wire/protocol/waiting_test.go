package protocol

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSessionSummary_WaitingWire(t *testing.T) {
	var zero SessionSummary
	b, err := json.Marshal(zero)
	require.NoError(t, err)
	require.NotContains(t, string(b), "waiting_on", "omitted when not waiting")
	require.NotContains(t, string(b), "waiting_since", "omitted when not waiting")

	at := time.Date(2026, 3, 10, 15, 30, 0, 0, time.UTC)
	b, err = json.Marshal(SessionSummary{State: StateWorking, WaitingOn: WaitShell, WaitingSince: at})
	require.NoError(t, err)
	require.Contains(t, string(b), `"waiting_on":"shell"`)
	require.Contains(t, string(b), `"waiting_since":"2026-03-10T15:30:00Z"`)

	var back SessionSummary
	require.NoError(t, json.Unmarshal(b, &back))
	require.Equal(t, WaitShell, back.WaitingOn)
	require.True(t, at.Equal(back.WaitingSince))

	// An older daemon never sends the fields.
	var old SessionSummary
	require.NoError(t, json.Unmarshal([]byte(`{"state":"working"}`), &old))
	require.Empty(t, old.WaitingOn)
	require.True(t, old.WaitingSince.IsZero())
}

func TestValidWaitKind(t *testing.T) {
	for _, k := range []string{WaitShell, WaitSubagent, WaitWeb} {
		require.True(t, ValidWaitKind(k), k)
	}
	for _, k := range []string{"", "Shell", "net", "shell ", "shell\n"} {
		require.False(t, ValidWaitKind(k), k)
	}
}

func TestSessionSummary_WaitingFor(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	since := func(d time.Duration) time.Time { return now.Add(-d) }
	cases := []struct {
		name string
		s    SessionSummary
		want string
	}{
		{"not waiting", SessionSummary{State: StateWorking}, ""},
		{"just started", SessionSummary{State: StateWorking, WaitingOn: WaitShell, WaitingSince: since(WaitingAfter - time.Millisecond)}, ""},
		{"at threshold", SessionSummary{State: StateWorking, WaitingOn: WaitShell, WaitingSince: since(WaitingAfter)}, WaitShell},
		{"long", SessionSummary{State: StateWorking, WaitingOn: WaitWeb, WaitingSince: since(time.Hour)}, WaitWeb},
		{"no since", SessionSummary{State: StateWorking, WaitingOn: WaitShell}, ""},
		{"future since", SessionSummary{State: StateWorking, WaitingOn: WaitShell, WaitingSince: now.Add(time.Minute)}, ""},
		{"unknown kind", SessionSummary{State: StateWorking, WaitingOn: "teleport", WaitingSince: since(time.Hour)}, ""},
		{"needs_input", SessionSummary{State: StateNeedsInput, WaitingOn: WaitShell, WaitingSince: since(time.Hour)}, ""},
		{"queued", SessionSummary{State: StateQueued, WaitingOn: WaitShell, WaitingSince: since(time.Hour)}, ""},
		{"done", SessionSummary{State: StateDone, WaitingOn: WaitShell, WaitingSince: since(time.Hour)}, ""},
		{"marked done", SessionSummary{State: StateWorking, RetireAt: now.Add(time.Hour), WaitingOn: WaitShell, WaitingSince: since(time.Hour)}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, c.s.WaitingFor(now))
		})
	}
}
