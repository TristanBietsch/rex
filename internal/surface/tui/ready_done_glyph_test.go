package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/termenv"
	"github.com/rivo/uniseg"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/width"

	"github.com/tristanbietsch/rex/internal/catalog/theme"
	"github.com/tristanbietsch/rex/internal/testutil/golden"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// markerStates are the states with a fixed marker (Working spins).
var markerStates = []protocol.State{
	protocol.StateNeedsInput, protocol.StateQueued, protocol.StateDone,
	protocol.StateFailed, protocol.StateCrashed,
}

// TestStateGlyphs: the v2.0.0 markers. Each is one cell in every width helper
// (East Asian narrow or neutral, never ambiguous; no emoji presentation), and
// no two states share one.
func TestStateGlyphs(t *testing.T) {
	require.Equal(t, "⚑", stateGlyph(protocol.StateNeedsInput))
	require.Equal(t, "✓", stateGlyph(protocol.StateDone))
	require.Equal(t, "✗", stateGlyph(protocol.StateFailed))
	require.Equal(t, "⊘", stateGlyph(protocol.StateCrashed))
	require.Equal(t, "◌", stateGlyph(protocol.StateQueued))
	require.Equal(t, "↻", loopDefault)
	wide := runewidth.NewCondition()
	wide.EastAsianWidth = true
	seen := map[string]protocol.State{}
	for _, st := range markerStates {
		g := stateGlyph(st)
		require.Len(t, []rune(g), 1, "%s %q", st, g)
		require.Equal(t, 1, ansi.StringWidth(g), "%s %q", st, g)
		require.Equal(t, 1, uniseg.StringWidth(g), "%s %q emoji?", st, g)
		require.Equal(t, 1, wide.StringWidth(g), "%s %q is ambiguous width", st, g)
		k := width.LookupRune([]rune(g)[0]).Kind()
		require.True(t, k == width.Neutral || k == width.EastAsianNarrow, "%s %q kind %v", st, g, k)
		if other, dup := seen[g]; dup {
			t.Fatalf("%s and %s share %q", st, other, g)
		}
		seen[g] = st
	}
}

// TestReadyDoneMarkersPerTheme: in every shipped theme Ready and Done markers
// are drawn in the theme's two greens (Needs and Done), and the glyphs differ,
// so the states part by color and by shape.
func TestReadyDoneMarkersPerTheme(t *testing.T) {
	defer applyTheme(theme.DefaultName)
	withProfile(termenv.TrueColor, func() {
		for _, name := range theme.Names() {
			require.True(t, applyTheme(name), name)
			p, _ := theme.Get(name)
			ready := stateMarkerCellFor(Model{}, protocol.StateNeedsInput, 0, false)
			done := stateMarkerCellFor(Model{}, protocol.StateDone, 0, false)
			require.NotEqual(t, ansi.Strip(ready), ansi.Strip(done), name)
			require.Equal(t, lipgloss.Color(p.Needs), styleMarker[protocol.StateNeedsInput].GetForeground(), name)
			require.Equal(t, lipgloss.Color(p.Done), styleMarker[protocol.StateDone].GetForeground(), name)
			require.Equal(t, lipgloss.Color(p.Needs), styleAlert.GetForeground(), "%s: the Ready title and count", name)
		}
	})
}

// TestGoldenReadyDoneNoColor: with no color at all, Ready rows still read
// apart from Done rows: every Ready row starts with ⚑, every done row with ✓.
// The golden is the plain 120-column board with all four sections.
func TestGoldenReadyDoneNoColor(t *testing.T) {
	sz := goldenSize{120, 30}
	m := goldenIcons(t, sz, false)
	sizeGolden(&m, sz)
	var body string
	withProfile(termenv.Ascii, func() { body = m.View() })
	require.Equal(t, body, ansi.Strip(body), "no SGR at all")
	section := ""
	var ready, done int
	for _, line := range strings.Split(body, "\n") {
		if t := strings.TrimSpace(line); t != "" && strings.ToUpper(t) == t && strings.Fields(t)[0] != "" {
			switch strings.Fields(t)[0] {
			case "READY", "WAITING", "WORKING", "DONE":
				section = strings.Fields(t)[0]
				continue
			}
		}
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "↳") || strings.HasPrefix(trimmed, "─") {
			section = "" // the preview line and the footer end the board
			continue
		}
		if len([]rune(line)) < 4 || strings.TrimSpace(line) == "" {
			continue
		}
		mark := string([]rune(line)[rowIndent()])
		switch section {
		case "READY":
			require.Equal(t, "⚑", mark, line)
			ready++
		case "DONE":
			require.Contains(t, []string{"✓", "✗", "⊘"}, mark, line)
			if mark == "✓" {
				done++
			}
		}
	}
	require.Positive(t, ready)
	require.Positive(t, done)
	golden.Check(t, "testdata/golden/board_ready_done_nocolor.txtar", golden.Section{Name: "plain.120x30", Body: body})
}
