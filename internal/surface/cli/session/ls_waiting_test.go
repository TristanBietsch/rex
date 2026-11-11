package session

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func waitingSessions() []protocol.SessionSummary {
	return []protocol.SessionSummary{
		{ID: "a91c0000", ShortID: "a91c", State: protocol.StateWorking, ToolID: "claude", ModelID: "opus",
			Slug: "run-tests", LastEventAt: lsNow.Add(-time.Minute),
			WaitingOn: protocol.WaitShell, WaitingSince: lsNow.Add(-42 * time.Second)},
		{ID: "7d4f0000", ShortID: "7d4f", State: protocol.StateWorking, ToolID: "claude", ModelID: "opus",
			Slug: "fetch-docs", LastEventAt: lsNow.Add(-time.Minute),
			WaitingOn: protocol.WaitWeb, WaitingSince: lsNow.Add(-time.Second)}, // under the threshold
		{ID: "0b2e0000", ShortID: "0b2e", State: protocol.StateWorking, ToolID: "claude", ModelID: "opus",
			Slug: "plain", LastEventAt: lsNow.Add(-time.Minute)},
		{ID: "1111", ShortID: "1111", State: protocol.StateWorking, ToolID: "claude", ModelID: "opus",
			Slug: "agents", LastEventAt: lsNow.Add(-time.Minute),
			WaitingOn: protocol.WaitSubagent, WaitingSince: lsNow.Add(-time.Hour)},
	}
}

func emitLsWaiting(t *testing.T, format string, waiting bool) string {
	t.Helper()
	ss := waitingSessions()
	p, out := lsPrinter(t, format, 100, false)
	require.NoError(t, p.Emit(core.Result{Cmd: "ls", Data: ss, View: lsView(ss, lsNow, p.Glyphs, lsOpts{Waiting: waiting})}))
	return out.String()
}

func TestLsWaitingStateLabel(t *testing.T) {
	text := emitLsWaiting(t, "text", true)
	require.Contains(t, text, "waiting (shell)")
	require.Contains(t, text, "waiting (subagent)")
	require.NotContains(t, text, "waiting (web)", "under 3 s it is still working")
	require.Equal(t, 2, strings.Count(text, "waiting ("))
	require.Contains(t, text, "2 waiting")
	require.Contains(t, text, "2 working")

	md := emitLsWaiting(t, "md", true)
	require.Contains(t, md, "waiting (shell)")
}

// Scripts that cut the txt columns, and agent state= keys, keep working.
func TestLsWaitingScriptFormatsKeepWorking(t *testing.T) {
	txt := emitLsWaiting(t, "txt", true)
	require.NotContains(t, txt, "waiting")
	require.Contains(t, txt, "working")

	agent := emitLsWaiting(t, "agent", true)
	require.NotContains(t, agent, "waiting")
	require.Equal(t, 4, strings.Count(agent, "state=working"))
}

func TestLsWaitingFlagOff(t *testing.T) {
	for _, f := range []string{"text", "md", "txt", "agent"} {
		out := emitLsWaiting(t, f, false)
		require.NotContains(t, out, "waiting (", f)
		require.NotContains(t, out, "2 waiting", f)
	}
}

func TestLsJSONCarriesWaitFields(t *testing.T) {
	ss := waitingSessions()[:1]
	p, out := lsPrinter(t, "json", 100, false)
	require.NoError(t, p.Emit(core.Result{Cmd: "ls", Data: ss, View: lsView(ss, lsNow, p.Glyphs, lsOpts{Waiting: true})}))
	require.Contains(t, out.String(), `"waiting_on":"shell"`)
	require.Contains(t, out.String(), `"waiting_since":`)
}
