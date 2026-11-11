package usage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

// testHub is an enabled hub on a fake clock whose goroutine never ticks by
// itself (tests call flush); it is closed with the test.
func testHub(t *testing.T) (*Hub, *Ledger, *clock) {
	t.Helper()
	clk := &clock{t: t0}
	l := newLedger(t)
	h := NewHub(context.Background(), HubConfig{Ledger: l, Now: clk.now, Tick: time.Hour})
	t.Cleanup(h.Close)
	h.SetEnabled(true)
	return h, l, clk
}

func (h *Hub) flush() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.flushLocked(false)
	// the lines are written by Sync in the caller
}

func sum(id string, state protocol.State, tokens int64) protocol.SessionSummary {
	return protocol.SessionSummary{ID: id, ToolID: "claude", ModelID: "opus", State: state, Tokens: tokens, StartedAt: t0}
}

func lines(t *testing.T, h *Hub, l *Ledger) int {
	t.Helper()
	h.Sync()
	t.Helper()
	b, err := os.ReadFile(l.Path())
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(t, err)
	n := 0
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}

// A live session gets a line at most every 10 minutes, and only when its
// totals changed.
func TestHub_PeriodicLinesAtMostEveryTenMinutes(t *testing.T) {
	h, l, clk := testHub(t)
	h.Observe(sum("a", protocol.StateWorking, 100))

	clk.advance(9 * time.Minute)
	h.flush()
	require.Zero(t, lines(t, h, l), "too soon")

	clk.advance(2 * time.Minute) // 11 min
	h.flush()
	require.Equal(t, 1, lines(t, h, l))

	h.Observe(sum("a", protocol.StateWorking, 500))
	clk.advance(5 * time.Minute)
	h.flush()
	require.Equal(t, 1, lines(t, h, l), "still inside the 10 minutes")

	clk.advance(6 * time.Minute)
	h.flush()
	require.Equal(t, 2, lines(t, h, l))

	clk.advance(20 * time.Minute)
	h.flush()
	require.Equal(t, 2, lines(t, h, l), "nothing changed: no repeat line")

	h.Sync()
	got, err := l.Latest()
	require.NoError(t, err)
	require.EqualValues(t, 500, got["a"].Tokens, "periodic lines do not add up")
}

func TestHub_EndWritesTheFinalLine(t *testing.T) {
	h, l, clk := testHub(t)
	h.Observe(sum("a", protocol.StateWorking, 100))
	h.Observe(sum("b", protocol.StateWorking, 7))
	clk.advance(11 * time.Minute)
	h.flush() // periodic lines for a and b

	done := sum("a", protocol.StateDone, 300)
	done.EndedAt = clk.t.Add(time.Minute)
	h.Observe(done)
	h.Observe(done) // a repeated event adds nothing

	h.Sync()
	got, err := l.Latest()
	require.NoError(t, err)
	require.True(t, got["a"].Ended)
	require.EqualValues(t, 300, got["a"].Tokens)
	require.Equal(t, done.EndedAt, got["a"].At, "an ended session is dated by when it ended")
	require.False(t, got["b"].Ended)
	require.Equal(t, 3, lines(t, h, l))
}

// Prune, retire and delete drop the session from the board; its line stays.
func TestHub_RemovedKeepsTheLedgerLine(t *testing.T) {
	h, l, _ := testHub(t)
	h.Observe(sum("a", protocol.StateWorking, 123))
	h.Removed("a") // killed by retire while working, no end event first

	h.Sync()
	got, err := l.Latest()
	require.NoError(t, err)
	require.EqualValues(t, 123, got["a"].Tokens)
	require.True(t, got["a"].Ended)

	// A session that already ended writes no second final line on removal.
	h.Observe(sum("b", protocol.StateDone, 5))
	n := lines(t, h, l)
	h.Removed("b")
	require.Equal(t, n, lines(t, h, l))
	h.Removed("never-seen")
	require.Equal(t, n, lines(t, h, l))
}

// A resumed session keeps its id: totals grow under it, not as a second one.
func TestHub_ResumedSessionKeepsItsID(t *testing.T) {
	h, l, clk := testHub(t)
	h.Observe(sum("a", protocol.StateWorking, 100))
	h.Observe(sum("a", protocol.StateDone, 100))
	clk.advance(time.Hour)
	h.Observe(sum("a", protocol.StateWorking, 100)) // resumed
	h.Observe(sum("a", protocol.StateWorking, 900))
	clk.advance(time.Hour)
	h.Observe(sum("a", protocol.StateDone, 900))

	h.Sync()
	got, err := l.Latest()
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.EqualValues(t, 900, got["a"].Tokens)
}

func TestHub_CostFromTheTapSumsConversationsAndPicksTheModel(t *testing.T) {
	h, l, _ := testHub(t)
	h.Observe(sum("a", protocol.StateWorking, 10))
	f := func(v float64) *float64 { return &v }
	h.Report("a", "conv1", f(1.5), "claude-opus-x")
	h.Report("a", "conv1", f(2.0), "") // the same conversation grew
	h.Report("a", "conv2", f(0.5), "") // /clear started another
	h.Report("unknown", "c", f(9), "m")
	h.Observe(sum("a", protocol.StateDone, 10))

	h.Sync()
	got, err := l.Latest()
	require.NoError(t, err)
	require.NotNil(t, got["a"].CostUSD)
	require.InDelta(t, 2.5, *got["a"].CostUSD, 1e-9)
	require.Equal(t, "claude-opus-x", got["a"].Model)
	require.NotContains(t, got, "unknown")
}

func TestHub_NoCostReportedMeansNoCost(t *testing.T) {
	h, l, _ := testHub(t)
	h.Observe(sum("a", protocol.StateDone, 10))
	h.Sync()
	got, err := l.Latest()
	require.NoError(t, err)
	require.Nil(t, got["a"].CostUSD)
	require.Equal(t, "opus", got["a"].Model)
}

// After a daemon restart the saved cost and tokens are the floor.
func TestHub_RestartKeepsTheSavedTotals(t *testing.T) {
	clk := &clock{t: t0}
	l := newLedger(t)
	c := 4.0
	r := rec("a", 800, t0.Add(-time.Hour))
	r.CostUSD = &c
	require.NoError(t, l.Append(r))

	h := NewHub(context.Background(), HubConfig{Ledger: l, Now: clk.now, Tick: time.Hour})
	t.Cleanup(h.Close)
	h.SetEnabled(true)
	h.Observe(sum("a", protocol.StateWorking, 100)) // the counter restarted low
	h.Report("a", "conv", func() *float64 { v := 1.0; return &v }(), "")
	h.Observe(sum("a", protocol.StateDone, 150))

	h.Sync()
	got, err := l.Latest()
	require.NoError(t, err)
	require.EqualValues(t, 800, got["a"].Tokens)
	require.InDelta(t, 4.0, *got["a"].CostUSD, 1e-9)
}

func TestHub_FirstEnableTrimsTheLedger(t *testing.T) {
	clk := &clock{t: t0}
	l := newLedger(t)
	require.NoError(t, l.Append(rec("old", 1, t0.Add(-100*24*time.Hour))))
	require.NoError(t, l.Append(rec("recent", 1, t0.Add(-24*time.Hour))))
	h := NewHub(context.Background(), HubConfig{Ledger: l, Now: clk.now, Tick: time.Hour})
	t.Cleanup(h.Close)
	h.SetEnabled(true)
	h.Sync()
	got, err := l.Latest()
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Contains(t, got, "recent")
}

// Close writes what changed since the last line, so a restart loses nothing.
func TestHub_CloseFlushesLiveSessions(t *testing.T) {
	h, l, _ := testHub(t)
	h.Observe(sum("a", protocol.StateWorking, 321))
	require.Zero(t, lines(t, h, l))
	h.Close()
	h.Sync()
	got, err := l.Latest()
	require.NoError(t, err)
	require.EqualValues(t, 321, got["a"].Tokens)
	require.False(t, got["a"].Ended)
	h.Close() // twice is fine
}

// Sessions that finished before the hub was switched on are recorded once,
// dated by when they ended, and not again when the ledger already has them.
func TestHub_EnableBackfillsFinishedSessionsOnce(t *testing.T) {
	clk := &clock{t: t0}
	l := newLedger(t)
	ended := sum("known", protocol.StateDone, 50)
	ended.EndedAt = t0.Add(-2 * time.Hour)
	fresh := sum("fresh", protocol.StateDone, 70)
	fresh.EndedAt = t0.Add(-3 * time.Hour)
	ancient := sum("ancient", protocol.StateDone, 9)
	ancient.EndedAt = t0.Add(-200 * 24 * time.Hour)
	r := rec("known", 50, ended.EndedAt)
	r.Ended = true
	require.NoError(t, l.Append(r))

	h := NewHub(context.Background(), HubConfig{Ledger: l, Now: clk.now, Tick: time.Hour,
		Sessions: func() []protocol.SessionSummary { return []protocol.SessionSummary{ended, fresh, ancient} }})
	t.Cleanup(h.Close)
	h.SetEnabled(true)

	h.Sync()
	got, err := l.Latest()
	require.NoError(t, err)
	require.Len(t, got, 2, "ancient is past the retention")
	require.Equal(t, fresh.EndedAt, got["fresh"].At)
	require.Equal(t, 2, lines(t, h, l), "known was not written again")
}

// Off is inert: no file, no directory, no goroutine.
func TestHub_OffIsInert(t *testing.T) {
	clk := &clock{t: t0}
	dir := filepath.Join(t.TempDir(), "share")
	l := OpenLedger(filepath.Join(dir, "usage.jsonl"))
	h := NewHub(context.Background(), HubConfig{Ledger: l, Now: clk.now, Tick: time.Millisecond,
		Sessions: func() []protocol.SessionSummary { t.Fatal("listed sessions while off"); return nil }})

	require.False(t, h.Enabled())
	h.Observe(sum("a", protocol.StateDone, 1))
	h.Report("a", "c", nil, "m")
	h.Removed("a")
	h.SetEnabled(false)
	clk.advance(time.Hour)
	h.Close()

	require.NoDirExists(t, dir)
	require.False(t, h.started, "no goroutine was ever started")
	(*Hub)(nil).Observe(sum("a", protocol.StateDone, 1)) // a nil hub is off too
	(*Hub)(nil).Close()
}

func TestHub_SwitchedOffStopsWriting(t *testing.T) {
	h, l, _ := testHub(t)
	h.Observe(sum("a", protocol.StateWorking, 1))
	h.SetEnabled(false)
	h.Observe(sum("a", protocol.StateDone, 2))
	h.Removed("a")
	require.Zero(t, lines(t, h, l))
	require.False(t, h.Enabled())
}
