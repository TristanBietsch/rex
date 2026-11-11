package session

import (
	"bytes"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil/golden"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

var lsNow = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

func lsSessions() []protocol.SessionSummary {
	return []protocol.SessionSummary{
		{ID: "a91c0000", ShortID: "a91c", State: protocol.StateNeedsInput, ToolID: "antigravity", ModelID: "gemini-3-pro",
			Slug: "refactor-db", LastEventAt: lsNow.Add(-12 * time.Second)},
		{ID: "7d4f0000", ShortID: "7d4f", State: protocol.StateWorking, ToolID: "claude", ModelID: "opus", Effort: "high",
			Slug: "fix-auth-flow", Name: "Auth fix", LastEventAt: lsNow.Add(-2 * time.Minute)},
		{ID: "0b2e0000", ShortID: "0b2e", State: protocol.StateDone, ToolID: "claude", ModelID: "sonnet", Effort: "medium",
			Slug: "write-tests", Name: "Tests", RetireAt: lsNow, LastEventAt: lsNow.Add(-time.Hour)},
		{ID: "1111", ShortID: "1111", State: protocol.StateQueued, ToolID: "echo", ModelID: "short",
			Slug: "a-slug-that-is-much-longer-than-twenty-four-cells", Name: "日本語の名前"},
		{ID: "2222", ShortID: "2222", State: protocol.StateFailed, ToolID: "echo", ModelID: "a-model-name-longer-than-20-cells",
			Slug: "日本語のスラッグ日本語のスラッグ日本語のスラッグ", LastEventAt: lsNow.Add(-50 * time.Hour)},
	}
}

func lsPrinter(t *testing.T, format string, width int, color bool) (*core.Printer, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	env := core.Env{Getenv: func(string) string { return "" }, OutTTY: color, ErrTTY: color,
		Width: width, Scheme: "default", TrueColor: true, GOOS: "linux"}
	p, err := core.NewPrinter(core.FormatFlags{Output: format}, env, &out, io.Discard)
	require.NoError(t, err)
	p.Now = func() time.Time { return lsNow }
	return p, &out
}

func emitLs(t *testing.T, format string, width int, color bool, sessions []protocol.SessionSummary) string {
	t.Helper()
	p, out := lsPrinter(t, format, width, color)
	require.NoError(t, p.Emit(core.Result{Cmd: "ls", Data: sessions, View: lsView(sessions, lsNow, p.Glyphs, lsOpts{})}))
	return out.String()
}

// The txt format is today's pipe output, byte for byte.
func TestLsTXTMatchesLegacyTableExactly(t *testing.T) {
	sessions := lsSessions()
	for i := range sessions {
		sessions[i].LastEventAt = time.Time{} // "-": the legacy writer reads the wall clock
	}
	cases := map[string][]protocol.SessionSummary{"mixed": sessions, "one": sessions[:1], "empty": {}, "nil": nil}
	for name, ss := range cases {
		var want bytes.Buffer
		require.NoError(t, core.WriteSessionsTable(&want, ss))
		require.Equal(t, want.String(), emitLs(t, "txt", 80, false, ss), name)
		require.Equal(t, want.String(), emitLs(t, "txt", 40, false, ss), name+": txt ignores width")
	}
}

func TestLsGolden(t *testing.T) {
	var secs []golden.Section
	for _, w := range []int{40, 80, 120} {
		secs = append(secs, golden.Section{Name: "text.w" + strconv.Itoa(w), Body: emitLs(t, "text", w, false, lsSessions())})
	}
	secs = append(secs,
		golden.Section{Name: "text.color.w80", Body: emitLs(t, "text", 80, true, lsSessions())},
		golden.Section{Name: "txt", Body: emitLs(t, "txt", 80, false, lsSessions())},
		golden.Section{Name: "md", Body: emitLs(t, "md", 80, false, lsSessions())},
		golden.Section{Name: "json", Body: emitLs(t, "json", 80, false, lsSessions()[:2])},
		golden.Section{Name: "agent", Body: emitLs(t, "agent", 80, false, lsSessions())},
		golden.Section{Name: "empty.text", Body: emitLs(t, "text", 80, false, nil)},
		golden.Section{Name: "empty.txt", Body: emitLs(t, "txt", 80, false, nil)},
		golden.Section{Name: "empty.agent", Body: emitLs(t, "agent", 80, false, nil)},
	)
	golden.Check(t, "testdata/golden/ls.txtar", secs...)
}

func TestLsTextFitsWidth(t *testing.T) {
	for _, w := range []int{40, 80, 120} {
		out := emitLs(t, "text", w, false, lsSessions())
		for _, l := range strings.Split(out, "\n")[2:] { // the header may exceed 40 cells
			require.LessOrEqual(t, displayWidth(l), w, "width %d: %q", w, l)
		}
	}
}

func TestLsFlattensUntrustedCells(t *testing.T) {
	evil := lsSessions()[:1]
	evil[0].Slug = "x\x1b]0;pwn\a-slug"
	evil[0].Name = "n\x1b[31mred\nline"
	for _, f := range []string{"text", "txt", "md", "json", "agent"} {
		out := emitLs(t, f, 80, false, evil)
		require.NotContains(t, out, "\x1b", f)
		require.NotContains(t, out, "\a", f)
		require.NotContains(t, out, "\nline", f)
	}
	// Legacy writer (today's table) is untouched by the kit; fleet still uses it.
	require.Contains(t, emitLs(t, "txt", 80, false, evil), "x-slug")
}

func TestLsAgentLines(t *testing.T) {
	out := emitLs(t, "agent", 80, false, lsSessions())
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	require.Equal(t, `session id=a91c state=needs_input tool=antigravity model=gemini-3-pro effort="" slug=refactor-db idle_s=12 name=""`, lines[0])
	require.Equal(t, `session id=7d4f state=working tool=claude model=opus effort=high slug=fix-auth-flow idle_s=120 name="Auth fix"`, lines[1])
	require.Contains(t, lines[3], "idle_s=-1")
	require.Equal(t, "ok cmd=ls count=5", lines[len(lines)-1])
}

func displayWidth(s string) int { return ansi.StringWidth(s) }
