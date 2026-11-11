package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The marker column is one cell wide: every frame must measure 1 in the
// width helpers the board uses, or the row grid shifts as it turns.
func TestWaitingGlyphsAreWidthOne(t *testing.T) {
	require.Equal(t, "◷", waitingGlyph)
	require.Equal(t, []string{"◴", "◷", "◶", "◵"}, waitingFrames)
	for _, g := range waitingFrames {
		require.Equal(t, 1, ansi.StringWidth(g), g)
		require.Equal(t, 1, lipgloss.Width(g), g)
		require.Equal(t, 1, lipglossWidth(g), g)
	}
}

func TestWaitingGlyph_TurnsOncePerSecond(t *testing.T) {
	m := waitingModel()
	m.SpinnerTick = 0
	require.Equal(t, "◷", waitingGlyphFor(m), "tick 0 (snapshots) shows the still glyph")

	var got []string
	for sec := 0; sec < 5; sec++ {
		for tick := 0; tick < waitingTicksPerStep; tick++ {
			m.SpinnerTick = sec*waitingTicksPerStep + tick
			if tick == 0 {
				got = append(got, waitingGlyphFor(m))
			} else {
				require.Equal(t, got[sec], waitingGlyphFor(m), "one step per second, not per tick")
			}
		}
	}
	require.Equal(t, []string{"◷", "◶", "◵", "◴", "◷"}, got)
}

func TestWaitingGlyph_StaticUnderReduceMotion(t *testing.T) {
	st := settings.NewStore()
	require.NoError(t, st.Set("reduce_motion", true))
	m := waitingModel()
	m.Store = st
	for _, tick := range []int{0, 10, 20, 30, 77} {
		m.SpinnerTick = tick
		require.Equal(t, "◷", waitingGlyphFor(m), tick)
	}
}

func TestRow_WaitingMarkerAndSpinner(t *testing.T) {
	m := waitingBoard()
	m.Detailed = true
	m.SpinnerTick = 10 // ◶
	row := func(slug string) string {
		for _, line := range strings.Split(ansi.Strip(renderBoard(m, 120, 0)), "\n") {
			fields := strings.Fields(line)
			for _, f := range fields {
				if f == slug {
					return line
				}
			}
		}
		t.Fatalf("no row %s", slug)
		return ""
	}
	require.Contains(t, row("w1"), "◶", "a waiting row turns")
	require.Contains(t, row("w2"), "◶")
	require.NotContains(t, row("r1"), "◶")
	// The row that has waited under 3 s still shows the working spinner.
	require.NotContains(t, row("q1"), "◶")
	for _, g := range waitingFrames {
		require.NotContains(t, row("q1"), g)
	}
	for _, g := range waitingFrames {
		require.NotContains(t, row("n1"), g)
		require.NotContains(t, row("d1"), g)
	}
}

// Every row, waiting or not, is exactly the board width at every frame.
func TestRow_WaitingKeepsGrid(t *testing.T) {
	for tick := 0; tick < 40; tick += waitingTicksPerStep {
		m := waitingBoard()
		m.SpinnerTick = tick
		for _, line := range strings.Split(renderBoard(m, 90, 0), "\n") {
			require.LessOrEqual(t, ansi.StringWidth(line), 90)
		}
		m.Detailed = true
		for _, line := range strings.Split(renderBoard(m, 120, 0), "\n") {
			require.LessOrEqual(t, ansi.StringWidth(line), 120)
		}
	}
}

func TestRow_DetailedShowsReasonAndAge(t *testing.T) {
	s := waiter("w1", protocol.WaitShell, 42*time.Second)
	s.Description = "Running the test suite"
	m := waitingModel(s)
	m.Detailed = true
	require.Contains(t, ansi.Strip(renderBoard(m, 140, 0)), "shell 42s · Running the test suite")

	s.WaitingOn, s.WaitingSince = protocol.WaitWeb, waitNow.Add(-5*time.Minute)
	s.Description = ""
	m = waitingModel(s)
	m.Detailed = true
	text := ansi.Strip(renderBoard(m, 140, 0))
	require.Contains(t, text, "web 5m")
	require.NotContains(t, text, "web 5m ·", "no dangling separator")

	// The simple view keeps the task text.
	m.Detailed = false
	require.NotContains(t, ansi.Strip(renderBoard(m, 140, 0)), "web 5m")
}

func TestRow_FlagOffShowsPlainWorking(t *testing.T) {
	m := waitingOff(waitingBoard())
	m.Detailed = true
	m.SpinnerTick = 10
	text := ansi.Strip(renderBoard(m, 120, 0))
	for _, g := range waitingFrames {
		require.NotContains(t, text, g)
	}
	require.NotContains(t, text, "shell 1m")
}

func TestAttachChip_Waiting(t *testing.T) {
	m := waitingBoard()
	s, _ := findSession(m.Sessions, "w1")
	m.Attach = &AttachState{SessionID: "w1", Slug: "w1"}
	bar := ansi.Strip(attachTitleBar(m, m.Attach, 100))
	require.Contains(t, bar, "waiting (shell)")
	require.NotContains(t, bar, "working")
	require.Equal(t, protocol.WaitShell, s.WaitingOn)

	m.Attach = &AttachState{SessionID: "q1", Slug: "q1"}
	require.Contains(t, ansi.Strip(attachTitleBar(m, m.Attach, 100)), "working", "under 3 s it is still working")
}
