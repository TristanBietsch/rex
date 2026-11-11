package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil/golden"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestMarqueeWindow(t *testing.T) {
	const text = "abcdefghij"
	step := bannerStep
	cases := []struct {
		name    string
		elapsed time.Duration
		want    string
	}{
		{"start", 0, "abcdef"},
		{"still paused", bannerPause - time.Millisecond, "abcdef"},
		{"pause over, one step", bannerPause + step, "bcdefg"},
		{"five steps", bannerPause + 5*step, "fghij "},
		{"inside the gap", bannerPause + 8*step, "ij   ·"},
		{"second copy", bannerPause + 13*step, "·   ab"},
		{"end of cycle pauses at the start again", bannerPause + 18*step, "abcdef"},
		{"and wraps for good", 2*(bannerPause+18*step) + step/2, "abcdef"},
	}
	for _, c := range cases {
		got := marqueeWindow(text, 6, c.elapsed)
		require.Equal(t, 6, ansi.StringWidth(got), c.name)
		require.Equal(t, c.want, got, c.name)
	}
}

func TestMarqueeWindow_WideRunesAtEdges(t *testing.T) {
	const text = "日本語のテスト文字列です"
	for i := range 40 {
		got := marqueeWindow(text, 7, bannerPause+time.Duration(i)*bannerStep)
		require.Equal(t, 7, ansi.StringWidth(got), "step %d: %q", i, got)
		require.NotContains(t, got, "�")
	}
}

func TestMarqueeWindow_Degenerate(t *testing.T) {
	require.Equal(t, "", marqueeWindow("abc", 0, 0))
	require.Equal(t, "   ", marqueeWindow("", 3, 0))
}

// longBannerSession is a session whose banner overflows a 106-cell row.
func longBannerSession() protocol.SessionSummary {
	s := bannerSession()
	s.Description = "Rewriting the invoice rounding tests, then moving on to the proration edge cases the reviewer flagged on Tuesday"
	return s
}

func TestBanner_MarqueeScrollsWithClock(t *testing.T) {
	m := bannerModel(t, 120, 40, longBannerSession())
	m.Attach.BannerSince = bannerNow
	frame := func(d time.Duration) string {
		m.Clock = func() time.Time { return bannerNow.Add(d) }
		return ansi.Strip(renderAttachBanner(m, m.Attach, m.Attach.Cols))
	}
	f0 := frame(0)
	require.Equal(t, m.Attach.Cols, ansi.StringWidth(f0))
	require.True(t, strings.HasPrefix(f0, "↳ Rewriting the invoice"), f0)
	require.Equal(t, f0, frame(bannerPause-time.Millisecond), "paused at the start")
	f1 := frame(bannerPause + 10*bannerStep)
	require.NotEqual(t, f0, f1)
	require.True(t, strings.HasPrefix(f1, "↳ "), "the arrow stays put: %q", f1)
	require.Equal(t, m.Attach.Cols, ansi.StringWidth(f1))
	require.Contains(t, f1, "rounding tests")
	for i := range 300 {
		require.Equal(t, m.Attach.Cols, ansi.StringWidth(frame(time.Duration(i)*bannerStep)), "frame %d", i)
	}
}

func TestBanner_FitsMeansStaticNoTick(t *testing.T) {
	s := bannerSession()
	s.Title = ""
	m := bannerModel(t, 120, 40, s)
	require.False(t, bannerOverflows(m), "a short banner fits")
	m, cmd := armBanner(m)
	require.Nil(t, cmd)
	require.False(t, m.bannerRunning)

	m = bannerModel(t, 120, 40, longBannerSession())
	require.True(t, bannerOverflows(m))
	require.True(t, m.wantsBannerTick())
	m, cmd = armBanner(m)
	require.NotNil(t, cmd)
	require.True(t, m.bannerRunning)
	_, cmd = armBanner(m)
	require.Nil(t, cmd, "one chain at a time")
}

func TestBanner_ReduceMotionStatic(t *testing.T) {
	m := bannerModel(t, 120, 40, longBannerSession())
	require.NoError(t, m.Store.Set("reduce_motion", true))
	require.False(t, m.wantsBannerTick())
	a := ansi.Strip(renderAttachBanner(m, m.Attach, m.Attach.Cols))
	m.Attach.BannerSince = bannerNow.Add(-time.Minute) // a marquee would have scrolled by now
	require.Equal(t, a, ansi.Strip(renderAttachBanner(m, m.Attach, m.Attach.Cols)), "no motion")
	require.Contains(t, a, "…", "cut at a word boundary like static")
}

func TestBanner_StaticModeNeverTicks(t *testing.T) {
	m := bannerModel(t, 120, 40, longBannerSession())
	require.NoError(t, m.Store.Set("attach_banner", "static"))
	require.False(t, m.wantsBannerTick())
}

func TestBannerTick_ChainStopsOnDetachAndSaver(t *testing.T) {
	m := bannerModel(t, 120, 40, longBannerSession())
	m, cmd := armBanner(m)
	require.NotNil(t, cmd)
	gen := m.bannerGen

	out, cmd := m.Update(bannerTickMsg{Gen: gen})
	m = out.(Model)
	require.NotNil(t, cmd, "still visible and overflowing: re-arms")
	require.True(t, m.bannerRunning)

	out, cmd = m.Update(bannerTickMsg{Gen: gen + 1})
	require.Nil(t, cmd, "a stale chain does nothing")
	m = out.(Model)

	m.saverOn = true
	out, cmd = m.Update(bannerTickMsg{Gen: gen})
	m = out.(Model)
	require.Nil(t, cmd)
	require.False(t, m.bannerRunning, "the saver stops the chain")

	m.saverOn = false
	m, cmd = armBanner(m)
	require.NotNil(t, cmd, "and it re-arms after")
	m.Attach, m.Focus = nil, FocusBoard
	out, cmd = m.Update(bannerTickMsg{Gen: m.bannerGen})
	require.Nil(t, cmd, "detached: the chain ends")
	require.False(t, out.(Model).bannerRunning)
}

func TestBannerTick_EndedAgentStops(t *testing.T) {
	m := bannerModel(t, 120, 40, longBannerSession())
	m.Attach.Ended = true
	require.False(t, m.wantsBannerTick())
}

func TestBanner_RestartsOnNewDescription(t *testing.T) {
	m := bannerModel(t, 120, 40, longBannerSession())
	m.Attach.BannerSince = bannerNow.Add(-time.Minute)

	apply := func(m Model, patch map[string]any) Model {
		data, err := json.Marshal(protocol.SessionUpdated{SessionID: "a", Patch: patch})
		require.NoError(t, err)
		out, _ := m.Update(DaemonEventMsg{Env: protocol.Envelope{Type: protocol.EventSessionUpdated, Data: data}})
		return out.(Model)
	}
	m = apply(m, map[string]any{"last_line": "same description, new line"})
	require.Equal(t, bannerNow.Add(-time.Minute), m.Attach.BannerSince, "other patches keep the loop")

	m = apply(m, map[string]any{"description": "Now doing something else entirely"})
	require.Equal(t, bannerNow, m.Attach.BannerSince, "a new description is read from its start")
}

func TestBanner_MarqueeSnapshot(t *testing.T) {
	m := bannerModel(t, 120, 40, longBannerSession())
	m.Attach.BannerSince = bannerNow.Add(-bannerPause - 24*bannerStep)
	_, _ = m.Attach.Term.Write([]byte("agent output\r\n"))
	out := ansi.Strip(m.View())
	lines := strings.Split(out, "\n")
	require.Len(t, lines, 40)
	for _, l := range lines {
		require.LessOrEqual(t, ansi.StringWidth(l), 120)
	}
	golden.Check(t, "testdata/attach_banner_marquee_frame.txtar", golden.Section{Name: "view", Body: out + "\n"})
}
