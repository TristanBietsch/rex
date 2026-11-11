package inspect

import (
	"bytes"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil/golden"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

var viewNow = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

func viewPrinter(t *testing.T, format string, width int, color bool) (*core.Printer, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	env := core.Env{Getenv: func(string) string { return "" }, OutTTY: color, ErrTTY: color,
		Width: width, Scheme: "default", TrueColor: true, GOOS: "linux"}
	p, err := core.NewPrinter(core.FormatFlags{Output: format}, env, &out, io.Discard)
	require.NoError(t, err)
	p.Now = func() time.Time { return viewNow }
	return p, &out
}

func emitView(t *testing.T, format string, width int, color bool, build func(p *core.Printer) core.Result) string {
	t.Helper()
	p, out := viewPrinter(t, format, width, color)
	require.NoError(t, p.Emit(build(p)))
	return out.String()
}

// Finished sessions only: a live one reads the wall clock for its duration.
func viewSessions() []protocol.SessionSummary {
	day := time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC)
	return []protocol.SessionSummary{
		{ID: "a91c0000", ShortID: "a91c", Slug: "refactor-db", ToolID: "claude", ModelID: "opus", State: protocol.StateDone,
			StartedAt: day, LastEventAt: day.Add(84 * time.Minute)},
		{ID: "7d4f0000", ShortID: "7d4f", Slug: "fix-auth-flow", ToolID: "claude", ModelID: "sonnet", State: protocol.StateFailed,
			StartedAt: day.Add(time.Hour), LastEventAt: day.Add(time.Hour + 20*time.Second)},
		{ID: "0b2e0000", ShortID: "0b2e", Slug: "日本語のスラッグ日本語のスラッグ日本語のスラッグ", ToolID: "antigravity", ModelID: "gemini-3-pro",
			State: protocol.StateCrashed, StartedAt: day.Add(2 * time.Hour), LastEventAt: day.Add(2*time.Hour + 58*time.Minute), Fleet: "alpha"},
	}
}

func goldenSections(t *testing.T, build func(p *core.Printer) core.Result) []golden.Section {
	var secs []golden.Section
	for _, w := range []int{40, 80, 120} {
		secs = append(secs, golden.Section{Name: "text.w" + strconv.Itoa(w), Body: emitView(t, "text", w, false, build)})
	}
	secs = append(secs, golden.Section{Name: "text.color.w80", Body: emitView(t, "text", 80, true, build)})
	for _, f := range []string{"txt", "md", "json", "agent"} {
		secs = append(secs, golden.Section{Name: f, Body: emitView(t, f, 80, false, build)})
	}
	return secs
}

func TestDigestGolden(t *testing.T) {
	day := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	dr := digestRange{start: day, end: day.AddDate(0, 0, 1)}
	out := buildDigestOutput(viewSessions(), day)
	build := func(down bool) func(p *core.Printer) core.Result {
		return func(p *core.Printer) core.Result {
			r := core.Result{Cmd: "digest", Data: digestResultData(out), View: digestView(out, down, dr, p.Glyphs)}
			if down {
				r.Meta = map[string]any{"source": "disk"}
			}
			return r
		}
	}
	secs := goldenSections(t, build(false))
	for _, s := range goldenSections(t, build(true)) {
		if s.Name == "text.w80" || s.Name == "txt" || s.Name == "json" || s.Name == "agent" {
			secs = append(secs, golden.Section{Name: "disk." + s.Name, Body: s.Body})
		}
	}
	empty := buildDigestOutput(nil, day)
	for _, f := range []string{"text", "txt", "agent"} {
		secs = append(secs, golden.Section{Name: "empty." + f, Body: emitView(t, f, 80, false, func(p *core.Printer) core.Result {
			return core.Result{Cmd: "digest", Data: digestResultData(empty), View: digestView(empty, false, dr, p.Glyphs)}
		})})
	}
	multi := digestRange{start: day.AddDate(0, 0, -6), end: day.AddDate(0, 0, 1)}
	secs = append(secs, golden.Section{Name: "multiday.text", Body: emitView(t, "text", 80, false, func(p *core.Printer) core.Result {
		return core.Result{Cmd: "digest", View: digestView(out, false, multi, p.Glyphs)}
	})})
	golden.Check(t, "testdata/golden/digest.txtar", secs...)
}

func TestStatsGolden(t *testing.T) {
	agg := aggregateSessions(viewSessions())
	build := func(all bool) func(p *core.Printer) core.Result {
		return func(p *core.Printer) core.Result {
			return core.Result{Cmd: "stats", Data: statsResultData(agg, all), View: statsView(agg, all)}
		}
	}
	secs := goldenSections(t, build(false))
	empty := aggregateSessions(nil)
	for _, f := range []string{"text", "txt", "agent"} {
		secs = append(secs, golden.Section{Name: "empty." + f, Body: emitView(t, f, 80, false, func(p *core.Printer) core.Result {
			return core.Result{Cmd: "stats", Data: statsResultData(empty, false), View: statsView(empty, false)}
		})})
	}
	golden.Check(t, "testdata/golden/stats.txtar", secs...)
}

func TestFleetGolden(t *testing.T) {
	rows := fleetCounts(viewSessions())
	require.Equal(t, []fleetRow{{"(none)", 2}, {"alpha", 1}}, rows)
	secs := goldenSections(t, func(p *core.Printer) core.Result {
		return core.Result{Cmd: "fleet", Data: rows, View: fleetListView(rows)}
	})
	golden.Check(t, "testdata/golden/fleet_ls.txtar", secs...)
}

// -o json keeps what -json always carried, versioned and in integer units.
func TestDigestAndStatsJSONEnvelope(t *testing.T) {
	day := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	out := buildDigestOutput(viewSessions(), day)
	d := digestResultData(out)
	require.Equal(t, int64((84*time.Minute + 20*time.Second + 58*time.Minute).Milliseconds()), d.Totals.ActiveMs)
	require.Equal(t, "claude", d.ByTool[0].Key)
	require.Equal(t, int64((84*time.Minute + 20*time.Second).Milliseconds()), d.ByTool[0].ActiveMs)

	agg := aggregateSessions(viewSessions())
	s := statsResultData(agg, false)
	require.Len(t, s.ByModel, 3)
	require.Equal(t, 3, s.Totals.Sessions)
}

func TestInspectFlattensUntrustedCells(t *testing.T) {
	evil := viewSessions()[:1]
	evil[0].Slug = "x\x1b]0;pwn\a-slug"
	evil[0].ModelID = "m\x1b[31m"
	day := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	dr := digestRange{start: day, end: day.AddDate(0, 0, 1)}
	out := buildDigestOutput(evil, day)
	for _, f := range []string{"text", "txt", "md", "agent"} {
		got := emitView(t, f, 80, false, func(p *core.Printer) core.Result {
			return core.Result{Cmd: "digest", View: digestView(out, false, dr, p.Glyphs)}
		})
		require.NotContains(t, got, "\x1b", f)
		require.NotContains(t, got, "\a", f)
	}
}
