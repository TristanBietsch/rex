package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/testutil/golden"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func lsWideSessions() []protocol.SessionSummary {
	ss := lsSessions()
	ss[0].StartedAt = time.Date(2026, 11, 11, 11, 40, 0, 0, time.UTC)
	ss[1].StartedAt = time.Date(2026, 11, 11, 9, 5, 0, 0, time.UTC)
	ss[2].StartedAt = time.Date(2026, 11, 10, 22, 0, 0, 0, time.UTC)
	ss[2].EndedAt = time.Date(2026, 11, 11, 11, 0, 0, 0, time.UTC)
	ss[1].ResumeID = "7d4f3c8a-1234-4abc-89ab-cdefabcdef01"
	ss[2].ResumeID = "0b2e9f10-5678-4def-8abc-0123456789ab"
	ss[3].ResumeID = "-x --flag" // never shown
	ss[4].StartedAt = time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC)
	ss[4].LastEventAt = time.Date(2026, 11, 9, 8, 30, 0, 0, time.UTC) // no ended_at: older daemon
	return ss
}

func emitLsOpts(t *testing.T, format string, width int, color bool, sessions []protocol.SessionSummary, opts lsOpts) string {
	t.Helper()
	p, out := lsPrinter(t, format, width, color)
	require.NoError(t, p.Emit(core.Result{Cmd: "ls", Data: sessions, View: lsView(sessions, lsNow, p.Glyphs, opts)}))
	return out.String()
}

func TestLsWideGolden(t *testing.T) {
	wide := lsOpts{Wide: true, Times: true, Resume: true}
	var secs []golden.Section
	for _, w := range []int{80, 120, 160} {
		secs = append(secs, golden.Section{Name: "text.w" + strconv.Itoa(w),
			Body: emitLsOpts(t, "text", w, false, lsWideSessions(), wide)})
	}
	secs = append(secs,
		golden.Section{Name: "txt", Body: emitLsOpts(t, "txt", 80, false, lsWideSessions(), wide)},
		golden.Section{Name: "md", Body: emitLsOpts(t, "md", 80, false, lsWideSessions(), wide)},
		golden.Section{Name: "agent", Body: emitLsOpts(t, "agent", 80, false, lsWideSessions(), wide)},
	)
	golden.Check(t, "testdata/golden/ls_wide.txtar", secs...)
}

// -wide puts STARTED and ENDED before NAME, which stays the last txt column.
func TestLsWideTXTKeepsNameLast(t *testing.T) {
	out := emitLsOpts(t, "txt", 80, false, lsWideSessions(), lsOpts{Wide: true, Times: true})
	lines := strings.Split(out, "\n")
	hdr := lines[0]
	require.Less(t, strings.Index(hdr, "STARTED"), strings.Index(hdr, "ENDED"))
	require.Less(t, strings.Index(hdr, "ENDED"), strings.Index(hdr, "NAME"))
	require.True(t, strings.HasSuffix(hdr, "NAME"))
	require.Contains(t, out, "2026-11-10 22:00  2026-11-11 11:00")
	require.Contains(t, lines[5], "2026-11-09 08:00  2026-11-09 08:30", "terminal session without ended_at falls back to its last event")
	require.Contains(t, lines[1], "2026-11-11 11:40  -", "a live session has no end")
}

// Without -wide, or with the timestamps feature off, the table is the one
// `rex ls` always printed.
func TestLsWideOffIsInert(t *testing.T) {
	base := emitLsOpts(t, "txt", 80, false, lsWideSessions(), lsOpts{})
	for _, o := range []lsOpts{{Wide: true}, {Times: true}, {Resume: true}} {
		require.Equal(t, base, emitLsOpts(t, "txt", 80, false, lsWideSessions(), o))
		for _, f := range []string{"text", "md", "agent"} {
			out := emitLsOpts(t, f, 160, false, lsWideSessions(), o)
			require.NotContains(t, out, "ended", f)
			require.NotContains(t, out, "7d4f3c8a-1234", f)
		}
	}
	require.NotContains(t, base, "STARTED")
}

func TestLsWideAgentTimesAreRFC3339(t *testing.T) {
	out := emitLsOpts(t, "agent", 80, false, lsWideSessions()[2:3], lsOpts{Wide: true, Times: true})
	require.Contains(t, out, "started=2026-11-10T22:00:00Z ended=2026-11-11T11:00:00Z name=Tests")
}

// RunLs reads the timestamps switch from config.yaml.
func TestRunLsWideHonorsTimestampsFeature(t *testing.T) {
	dir := t.TempDir()
	d := testutil.StartDaemon(t)
	t.Cleanup(d.Close)

	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	defer c.Close()
	_, err = c.Hello("ls-wide-test")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	env, err := c.Do(ctx, protocol.IntentNewSession, protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "finished", CWD: dir,
	})
	require.NoError(t, err)
	var ack protocol.Ack
	require.NoError(t, json.Unmarshal(env.Data, &ack))
	require.Eventually(t, func() bool {
		sc, err := client.Dial(d.Socket)
		require.NoError(t, err)
		defer sc.Close()
		snap, err := sc.Hello("ls-wide-poll")
		require.NoError(t, err)
		for _, s := range snap.Sessions {
			if s.ID == ack.SessionID {
				return s.State == protocol.StateDone
			}
		}
		return false
	}, 15*time.Second, 100*time.Millisecond)

	run := func() string {
		p, out := lsPrinter(t, "txt", 80, false)
		core.SetOut(p)
		t.Cleanup(func() { core.SetOut(nil) })
		require.NoError(t, RunLs([]string{"-socket", d.Socket, "-wide"}))
		return out.String()
	}
	require.Contains(t, run(), "STARTED")

	cfg, err := paths.ConfigFile()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("timestamps: false\n"), 0o600))
	t.Cleanup(func() { _ = os.Remove(cfg) })
	out := run()
	require.NotContains(t, out, "STARTED")
	require.NotContains(t, out, "ENDED")
}
