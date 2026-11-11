package inspect

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil/golden"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func statusSessions(states ...protocol.State) []protocol.SessionSummary {
	var out []protocol.SessionSummary
	for _, s := range states {
		out = append(out, protocol.SessionSummary{State: s})
	}
	return out
}

func emitStatus(t *testing.T, format string, color bool, sessions []protocol.SessionSummary) string {
	t.Helper()
	var out bytes.Buffer
	env := core.Env{Getenv: func(string) string { return "" }, OutTTY: color, ErrTTY: color,
		Width: 80, Scheme: "default", TrueColor: true, GOOS: "linux"}
	p, err := core.NewPrinter(core.FormatFlags{Output: format}, env, &out, io.Discard)
	require.NoError(t, err)
	c := countSessions(sessions)
	require.NoError(t, p.Emit(core.Result{Cmd: "status", Data: c, View: statusView(c, sessions, p.Glyphs)}))
	return out.String()
}

func TestStatusTXTMatchesLegacyLine(t *testing.T) {
	cases := [][]protocol.SessionSummary{
		nil,
		statusSessions(protocol.StateDone, protocol.StateDone, protocol.StateWorking),
		statusSessions(protocol.StateNeedsInput, protocol.StateFailed, protocol.StateCrashed, protocol.StateQueued),
	}
	for _, ss := range cases {
		var want bytes.Buffer
		require.NoError(t, core.WriteAggregateLine(&want, ss))
		require.Equal(t, want.String(), emitStatus(t, "txt", false, ss))
	}
	require.Equal(t, "0 awaiting input · 1 working · 2 completed\n",
		emitStatus(t, "txt", false, cases[1]))
}

func TestStatusCounts(t *testing.T) {
	c := countSessions(statusSessions(protocol.StateNeedsInput, protocol.StateFailed, protocol.StateCrashed,
		protocol.StateQueued, protocol.StateDone, protocol.StateWorking))
	require.Equal(t, statusCounts{NeedsInput: 1, Working: 1, Queued: 1, Done: 1, Failed: 2}, c)
}

func TestStatusAgentAndJSON(t *testing.T) {
	ss := statusSessions(protocol.StateNeedsInput, protocol.StateWorking, protocol.StateDone, protocol.StateDone)
	require.Equal(t, "ok cmd=status needs_input=1 working=1 queued=0 done=2 failed=0\n", emitStatus(t, "agent", false, ss))
	require.Equal(t, `{"v":1,"cmd":"status","data":{"needs_input":1,"working":1,"queued":0,"done":2,"failed":0}}`+"\n",
		emitStatus(t, "json", false, ss))
}

func TestStatusGolden(t *testing.T) {
	mixed := statusSessions(protocol.StateNeedsInput, protocol.StateWorking, protocol.StateDone, protocol.StateDone,
		protocol.StateQueued, protocol.StateFailed)
	quiet := statusSessions(protocol.StateDone)
	golden.Check(t, "testdata/golden/status.txtar",
		golden.Section{Name: "text", Body: emitStatus(t, "text", false, mixed)},
		golden.Section{Name: "text.color", Body: emitStatus(t, "text", true, mixed)},
		golden.Section{Name: "text.quiet", Body: emitStatus(t, "text", false, quiet)},
		golden.Section{Name: "txt", Body: emitStatus(t, "txt", false, mixed)},
		golden.Section{Name: "md", Body: emitStatus(t, "md", false, mixed)},
		golden.Section{Name: "json", Body: emitStatus(t, "json", false, mixed)},
		golden.Section{Name: "agent", Body: emitStatus(t, "agent", false, mixed)},
	)
}
