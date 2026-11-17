package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestScanHead_PingPongsInAndOut(t *testing.T) {
	const n = 40
	span := n + 2*scanHalf
	sweep := min(max(time.Duration(span)*scanCellTime, scanMinSweep), scanMaxSweep)
	require.Equal(t, -scanHalf, scanHead(n, 0), "starts off the left edge")
	require.Equal(t, n-1+scanHalf, scanHead(n, sweep), "turns off the right edge")
	require.Equal(t, -scanHalf, scanHead(n, 2*sweep), "back where it began")
	require.Equal(t, scanHead(n, sweep/3), scanHead(n, 2*sweep-sweep/3), "the way back mirrors the way out")
	prev := scanHead(n, 0)
	for d := time.Duration(0); d <= sweep; d += 10 * time.Millisecond {
		h := scanHead(n, d)
		require.GreaterOrEqual(t, h, prev, "moves right during the first pass")
		prev = h
	}
	require.Equal(t, -scanHalf-1, scanHead(0, time.Second), "empty text: no head")
}

func TestScanHead_SweepBounds(t *testing.T) {
	// A one-cell line still takes scanMinSweep; a huge one scanMaxSweep.
	require.Equal(t, scanHalf, scanHead(1, scanMinSweep), "n=1: last cell 0, plus the half band")
	require.Equal(t, 1000-1+scanHalf, scanHead(1000, scanMaxSweep))
}

func TestScanTier(t *testing.T) {
	for d, want := range map[int]int{0: 0, 1: 1, -1: 1, 2: 2, -3: 2, 4: 3, -9: 3} {
		require.Equal(t, want, scanTier(d), "d=%d", d)
	}
}

func TestRenderScan_KeepsTextAndColorsOnlyTheBand(t *testing.T) {
	text := "Editing api/webhooks.go"
	base := lipgloss.NewStyle()
	got := renderScan(text, 0, 5, lipgloss.Color("#FF0000"), base)
	require.Equal(t, text, ansi.Strip(got), "styling never changes the text")
	off := renderScan(text, 0, -100, lipgloss.Color("#FF0000"), base)
	require.Equal(t, text, off, "head far away: all base, base is unstyled")
	// The offset shifts the band: head 5 at offset 0 equals head 15 at offset 10.
	require.Equal(t, got, renderScan(text, 10, 15, lipgloss.Color("#FF0000"), base))
}

func scanPreviewModel(state protocol.State) Model {
	s := described("a", "Running the billing tests after the rounding fix")
	s.State = state
	s.DescriptionAt = time.Time{} // no age: frames differ only by the band
	return previewModel(30, s)
}

func TestScanActive_OnlyWorkingOrWaiting(t *testing.T) {
	for state, want := range map[protocol.State]bool{
		protocol.StateWorking: true, protocol.StateQueued: true,
		protocol.StateNeedsInput: false, protocol.StateDone: false,
		protocol.StateFailed: false, protocol.StateCrashed: false,
	} {
		m := scanPreviewModel(state)
		require.Equal(t, want, scanActive(m, m.Sessions[0]), state)
		require.Equal(t, want, m.scanning(), state)
	}
	waiting := scanPreviewModel(protocol.StateWorking)
	waiting.Sessions[0].WaitingOn = "shell"
	waiting.Sessions[0].WaitingSince = previewNow.Add(-time.Minute)
	require.True(t, scanActive(waiting, waiting.Sessions[0]), "waiting on a tool")

	done := scanPreviewModel(protocol.StateWorking)
	done.Sessions[0].RetireAt = previewNow.Add(time.Hour)
	require.False(t, scanActive(done, done.Sessions[0]), "marked done")

	for _, id := range []string{"summary_scan", "reduce_motion"} {
		m := scanPreviewModel(protocol.StateWorking)
		val := id == "reduce_motion"
		require.NoError(t, m.Store.Set(id, val))
		require.False(t, scanActive(m, m.Sessions[0]), id)
		require.False(t, m.scanning(), id)
	}
}

func TestScanning_ArmsTheTickForAQueuedSession(t *testing.T) {
	m := scanPreviewModel(protocol.StateQueued)
	require.False(t, m.hasWorkingSessions())
	require.True(t, m.wantsSpinnerTick(), "the scanner moves on the spinner chain")
}

func TestPreview_ScanStylesButKeepsText(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor) // so styles emit escapes
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	working := scanPreviewModel(protocol.StateWorking)
	needs := scanPreviewModel(protocol.StateNeedsInput)
	for _, m := range []Model{working, needs} {
		require.Contains(t, ansi.Strip(strings.Join(renderDescPreview(m, 100), "\n")), "↳ Running the billing tests")
	}
	// Over a sweep the scanning line changes; the still one never does.
	frames := func(m Model) map[string]bool {
		seen := map[string]bool{}
		for d := time.Duration(0); d < 2*scanMaxSweep; d += 100 * time.Millisecond {
			at := previewNow.Add(d)
			m.Clock = func() time.Time { return at }
			seen[strings.Join(renderDescPreview(m, 100), "\n")] = true
		}
		return seen
	}
	require.Greater(t, len(frames(working)), 5)
	require.Len(t, frames(needs), 1)
}

func TestScanColor(t *testing.T) {
	m := scanPreviewModel(protocol.StateWorking)
	require.Equal(t, colorAccent, scanColor(m), "default: the accent")
	require.NoError(t, m.Store.Set("summary_scan_color", "living-coral"))
	named := scanColor(m)
	require.NotEqual(t, lipgloss.Color(""), named)
	require.NoError(t, m.Store.Set("summary_scan_color", "#00ff88"))
	custom := scanColor(m)
	require.NotEqual(t, named, custom)
	require.True(t, strings.HasPrefix(string(custom), "#"), custom)
	require.NoError(t, m.Store.Set("summary_scan_color", "accent"))
	require.Equal(t, colorAccent, scanColor(m))
}
