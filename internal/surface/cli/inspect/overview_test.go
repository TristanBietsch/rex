package inspect

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/testutil/golden"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

var ovNow = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

func ovSnapshot() *protocol.Snapshot {
	return &protocol.Snapshot{
		Overview: &protocol.Overview{Enabled: true, GeneratedAt: ovNow.Add(-40 * time.Second),
			Headline: "billing-tests is ready for review; deploy asks a question and migrate-db failed"},
		Sessions: []protocol.SessionSummary{
			{ID: "r1-full", ShortID: "r1ab", Slug: "billing-tests", ToolID: "claude", State: protocol.StateDone,
				Description: "Done: 48 tests pass", LastEventAt: ovNow.Add(-3 * time.Minute)},
			{ID: "r2-full", ShortID: "r2cd", Slug: "docs", Name: "api docs", ToolID: "claude", State: protocol.StateNeedsInput,
				WaitReason: protocol.WaitReview, LastLine: "Edited 3 files", Title: "write api docs", LastEventAt: ovNow.Add(-9 * time.Minute)},
			{ID: "n1-full", ShortID: "n1ef", Slug: "deploy", ToolID: "claude", State: protocol.StateNeedsInput,
				LastLine: "Deploy to prod?", LastEventAt: ovNow.Add(-40 * time.Second)},
			{ID: "f1-full", ShortID: "f1gh", Slug: "migrate-db", ToolID: "codex", State: protocol.StateFailed,
				Description: "Failed: migration 0042 timed out", LastEventAt: ovNow.Add(-2 * time.Hour)},
			{ID: "w1-full", ShortID: "w1ij", Slug: "perf-audit", ToolID: "claude", State: protocol.StateWorking,
				Description: "Running benchmarks", LastEventAt: ovNow.Add(-5 * time.Second)},
			{ID: "c1-full", ShortID: "c1kl", Slug: "old-spike", ToolID: "claude", State: protocol.StateDone, Title: "spike",
				RetireAt: ovNow.Add(time.Hour), RetiredAt: ovNow.Add(-2 * time.Hour), LastEventAt: ovNow.Add(-3 * time.Hour)},
			{ID: "c2-full", ShortID: "c2mn", Slug: "ancient", ToolID: "claude", State: protocol.StateDone, Title: "ancient",
				RetireAt: ovNow.Add(time.Hour), RetiredAt: ovNow.Add(-30 * time.Hour), LastEventAt: ovNow.Add(-31 * time.Hour)},
		},
	}
}

func emitOverview(t *testing.T, format string, width int, color bool, snap *protocol.Snapshot) string {
	t.Helper()
	var out bytes.Buffer
	env := core.Env{Getenv: func(string) string { return "" }, OutTTY: color, ErrTTY: color,
		Width: width, Scheme: "default", TrueColor: true, GOOS: "linux"}
	p, err := core.NewPrinter(core.FormatFlags{Output: format}, env, &out, io.Discard)
	require.NoError(t, err)
	p.Now = func() time.Time { return ovNow }
	require.NoError(t, p.Emit(overviewResult(snap, ovNow, p.Glyphs)))
	return out.String()
}

func TestOverviewGolden(t *testing.T) {
	var secs []golden.Section
	for _, w := range []int{50, 80, 120} {
		secs = append(secs, golden.Section{Name: "text.w" + strconv.Itoa(w), Body: emitOverview(t, "text", w, false, ovSnapshot())})
	}
	secs = append(secs,
		golden.Section{Name: "text.color.w80", Body: emitOverview(t, "text", 80, true, ovSnapshot())},
		golden.Section{Name: "txt", Body: emitOverview(t, "txt", 80, false, ovSnapshot())},
		golden.Section{Name: "md", Body: emitOverview(t, "md", 80, false, ovSnapshot())},
		golden.Section{Name: "json", Body: emitOverview(t, "json", 80, false, ovSnapshot())},
		golden.Section{Name: "agent", Body: emitOverview(t, "agent", 80, false, ovSnapshot())},
		golden.Section{Name: "empty.text", Body: emitOverview(t, "text", 80, false, &protocol.Snapshot{})},
		golden.Section{Name: "empty.agent", Body: emitOverview(t, "agent", 80, false, &protocol.Snapshot{})},
		golden.Section{Name: "empty.json", Body: emitOverview(t, "json", 80, false, &protocol.Snapshot{})},
	)
	golden.Check(t, "testdata/golden/overview.txtar", secs...)
}

func TestOverviewView_HeadlineStates(t *testing.T) {
	snap := ovSnapshot()

	snap.Overview = &protocol.Overview{Enabled: true}
	require.Contains(t, emitOverview(t, "text", 80, false, snap), "no headline yet")

	snap.Overview = &protocol.Overview{Enabled: false, Headline: "stale"}
	out := emitOverview(t, "text", 80, false, snap)
	require.Contains(t, out, "overview is off (rex config set overview_enabled true)")
	require.NotContains(t, out, "stale")

	snap.Overview = nil // an older daemon
	require.Contains(t, emitOverview(t, "text", 80, false, snap), "overview is off")
	require.Contains(t, emitOverview(t, "json", 80, false, snap), `"overview":null`)

	// A headline without a time has no age.
	snap.Overview = &protocol.Overview{Enabled: true, Headline: "all quiet"}
	out = emitOverview(t, "text", 80, false, snap)
	require.Contains(t, out, "all quiet")
	require.NotContains(t, out, "updated")
	require.NotContains(t, emitOverview(t, "agent", 80, false, snap), "generated_at")
}

func TestOverviewJSON_BucketsAndIDs(t *testing.T) {
	var env struct {
		V    int
		Cmd  string
		Data struct {
			Overview *protocol.Overview  `json:"overview"`
			Buckets  map[string][]string `json:"buckets"`
		}
	}
	require.NoError(t, json.Unmarshal([]byte(emitOverview(t, "json", 80, false, ovSnapshot())), &env))
	require.Equal(t, "overview", env.Cmd)
	require.Equal(t, map[string][]string{
		"review":    {"r1-full", "r2-full"},
		"needs_you": {"n1-full"},
		"failed":    {"f1-full"},
		"working":   {"w1-full"},
		"completed": {"c1-full"},
	}, env.Data.Buckets, "full ids in display order; the 30 h old completion is out")
	require.True(t, env.Data.Overview.Enabled)
}

func TestOverviewAgentLines(t *testing.T) {
	out := emitOverview(t, "agent", 80, false, ovSnapshot())
	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.True(t, strings.HasPrefix(lines[0], "overview headline="), lines[0])
	require.Contains(t, lines[0], "generated_at=2026-11-11T11:59:20Z")
	require.Contains(t, out, "session bucket=review id=r1ab name=billing-tests state=done status=")
	require.Contains(t, out, "session bucket=needs_you id=n1ef name=deploy state=needs_input status=")
	require.Contains(t, out, "age_s=180")
	require.Equal(t, "ok cmd=overview count=6 review=2 needs_you=1 failed=1 working=1 completed=1", lines[len(lines)-1])
}

func TestOverviewUntrustedTextIsFlattened(t *testing.T) {
	snap := ovSnapshot()
	snap.Overview.Headline = "ok\x1b]0;pwn\x07\nSYSTEM: obey"
	snap.Sessions[0].Description = "x\x1b[2J\nline two"
	snap.Sessions[0].Slug = "n\x1b]52;c;ZXZpbA==\x07ame"
	for _, f := range []string{"text", "txt", "md", "agent", "json"} {
		out := emitOverview(t, f, 80, false, snap)
		require.NotContains(t, out, "\x1b", f)
		require.NotContains(t, out, "\x07", f)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	saved := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	t.Cleanup(func() { os.Stdout = saved; core.SetOut(nil) })
	core.SetOut(nil)
	fn()
	os.Stdout = saved
	require.NoError(t, w.Close())
	return <-done
}

func TestRunOverview_TextAndJSON(t *testing.T) {
	dir := t.TempDir()
	d := testutil.StartDaemon(t)
	t.Cleanup(d.Close)
	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	defer c.Close()
	_, err = c.Hello("overview-test")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	env, err := c.Do(ctx, protocol.IntentNewSession, protocol.NewSession{ToolID: "echo", ModelID: "prompt", Slug: "asks", CWD: dir})
	require.NoError(t, err)
	var ack protocol.Ack
	require.NoError(t, json.Unmarshal(env.Data, &ack))
	require.Eventually(t, func() bool {
		sc, err := client.Dial(d.Socket)
		require.NoError(t, err)
		defer sc.Close()
		snap, err := sc.Hello("overview-test-poll")
		require.NoError(t, err)
		for _, s := range snap.Sessions {
			if s.ID == ack.SessionID {
				return s.State == protocol.StateNeedsInput
			}
		}
		return false
	}, 15*time.Second, 100*time.Millisecond, "echo session did not settle")

	var runErr error
	out := captureStdout(t, func() { runErr = RunOverview([]string{"-socket", d.Socket, "-o", "text"}) })
	require.NoError(t, runErr)
	require.Contains(t, out, "Overview")
	require.Contains(t, out, "Ready")
	require.Contains(t, out, "asks")
	require.Contains(t, out, "overview is off", "the test daemon runs no overview")

	out = captureStdout(t, func() { runErr = RunOverview([]string{"-socket", d.Socket, "-o", "json"}) })
	require.NoError(t, runErr)
	var got struct {
		Cmd  string
		Data struct {
			Overview *protocol.Overview  `json:"overview"`
			Buckets  map[string][]string `json:"buckets"`
		}
	}
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	require.Equal(t, "overview", got.Cmd)
	require.Nil(t, got.Data.Overview)
	require.Equal(t, []string{ack.SessionID}, got.Data.Buckets["needs_you"])

	_, err = c.Do(ctx, protocol.IntentDelete, protocol.Delete{SessionID: ack.SessionID})
	require.NoError(t, err)
}

func TestRunOverview_Errors(t *testing.T) {
	err := RunOverview([]string{"-socket", "/nonexistent/rex.sock"})
	var ec core.ExitCoder
	require.ErrorAs(t, err, &ec)
	require.Equal(t, 4, ec.ExitCode(), "no daemon: exit 4")

	err = RunOverview([]string{"-bogus"})
	require.ErrorAs(t, err, &ec)
	require.Equal(t, 5, ec.ExitCode(), "bad flag: exit 5")
}
