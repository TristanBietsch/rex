package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// helpModel is the board with the help overlay open on a w x h terminal.
func helpModel(w, h int) Model {
	m := Model{Focus: FocusHelp, Width: w, Height: h, Filter: "all", Store: settings.NewStore(), Help: &HelpState{}}
	return m
}

func helpPress(m Model, keys ...string) Model {
	for _, k := range keys {
		next, _ := m.Update(key(k))
		m = next.(Model)
	}
	return m
}

func helpText(m Model) string { return ansi.Strip(m.View()) }

// Every group is reachable by scrolling, whatever the terminal: nothing is
// dropped, and the cut edges say so.
func TestHelpScrolls(t *testing.T) {
	m := helpModel(50, 20)
	top := helpText(m)
	require.Contains(t, top, "Navigation")
	require.Contains(t, top, "↓ more")
	require.NotContains(t, top, "↑ more")
	require.Equal(t, 20, len(strings.Split(top, "\n")))

	seen := top
	for range 200 {
		m = helpPress(m, "j")
		seen += helpText(m)
	}
	bottom := helpText(m)
	require.Contains(t, bottom, "↑ more")
	require.NotContains(t, bottom, "↓ more")
	for _, g := range append(helpLeft(m), append(helpRight, helpLegend(m))...) {
		require.Contains(t, seen, g.Title, "group %q is reachable", g.Title)
	}
	for _, c := range commands {
		if c.Flag == "" || m.featureOn(c.Flag) {
			require.Contains(t, seen, c.helpKey(), "command %v is reachable", c.Names)
		}
	}

	m = helpPress(m, "g")
	require.Equal(t, 0, m.Help.Scroll)
	m = helpPress(m, "G")
	require.Positive(t, m.Help.Scroll)
	m = helpPress(m, "k")
	scrolled := m.Help.Scroll
	m = helpPress(m, "pgup")
	require.Less(t, m.Help.Scroll, scrolled)
}

func TestHelpWheelScrolls(t *testing.T) {
	m := helpModel(50, 20)
	next, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	require.Positive(t, next.(Model).Help.Scroll)
}

// `/` filters rows by key or description; esc clears the filter first and
// only then closes.
func TestHelpFilter(t *testing.T) {
	m := helpModel(120, 40)
	m = helpPress(m, "/", "p", "r", "u", "n", "e")
	require.True(t, m.Help.Filtering)
	text := helpText(m)
	require.Contains(t, text, ":prune")
	require.NotContains(t, text, ":export")
	rows := 0
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, "clear Done") {
			rows++
		}
	}
	require.Equal(t, 1, rows, "one row matches prune")
	require.Contains(t, text, "prune", "the filter shows in the panel")

	m = helpPress(m, "enter")
	require.False(t, m.Help.Filtering)
	require.Equal(t, "prune", m.Help.Filter)

	m = helpPress(m, "esc")
	require.Equal(t, FocusHelp, m.Focus, "first esc clears the filter")
	require.Empty(t, m.Help.Filter)
	require.Contains(t, helpText(m), ":export")

	m = helpPress(m, "esc")
	require.Equal(t, FocusBoard, m.Focus, "second esc closes")
}

func TestHelpFilterMatchesDescriptionAndNoMatch(t *testing.T) {
	m := helpPress(helpModel(120, 40), "/", "r", "e", "s", "u", "m", "e")
	text := helpText(m)
	require.Contains(t, text, ":uc :resume")
	require.NotContains(t, text, ":prune")

	m = helpPress(helpModel(120, 40), "/", "z", "z", "z", "z")
	require.Contains(t, helpText(m), "no match")
}

func TestHelpFilterEditing(t *testing.T) {
	m := helpPress(helpModel(120, 40), "/", "a", "b", "backspace")
	require.Equal(t, "a", m.Help.Filter)
	m = helpPress(m, "ctrl+u")
	require.Empty(t, m.Help.Filter)
	m = helpPress(m, "esc")
	require.False(t, m.Help.Filtering)
	require.Equal(t, FocusHelp, m.Focus, "esc ends filtering without closing")
}

// The legend draws the real markers in their real styles.
func TestHelpLegendUsesRealStyles(t *testing.T) {
	withProfile(termenv.TrueColor, func() {
		m := helpModel(120, 60)
		m.SpinnerTick = 0
		legend := helpLegend(m)
		require.Equal(t, helpGroupLegend, legend.Title)
		require.True(t, legend.Styled)
		keys := map[string]string{}
		for _, r := range legend.Rows {
			keys[ansi.Strip(r.Key)] = r.Key
		}
		frames := spinnerFramesFor(m)
		for glyph, st := range map[string]protocol.State{
			"◆": protocol.StateNeedsInput, frames[0]: protocol.StateWorking, queuedGlyph: protocol.StateQueued,
			"●": protocol.StateDone, "✕": protocol.StateFailed, "○": protocol.StateCrashed,
		} {
			require.Equal(t, styleMarker[st].Render(glyph), keys[glyph], "%s marker", st)
		}
		require.Equal(t, styleFleet[0].Render("▍"), keys["▍"])
		for i, badge := range []string{"▴ 6h", "▴ 2h", "▴ 30m", "▴ 10m"} {
			require.Equal(t, styleLongRun[i].Render(badge), keys[badge], "tier %d", i)
		}
		require.Contains(t, m.View(), styleMarker[protocol.StateDone].Render("●"), "the overlay carries the styled legend")
	})
}

func TestHelpLegendOmitsWaitingWhenOff(t *testing.T) {
	m := helpModel(120, 60)
	var on, off []string
	for _, r := range helpLegend(m).Rows {
		on = append(on, ansi.Strip(r.Key))
	}
	for _, r := range helpLegend(waitingOff(m)).Rows {
		off = append(off, ansi.Strip(r.Key))
	}
	require.Contains(t, on, waitingGlyph)
	require.NotContains(t, off, waitingGlyph)
}

// A key whose feature is off is still listed, marked, so the overlay
// explains why it does nothing.
func TestHelpMarksFeaturesThatAreOff(t *testing.T) {
	m := helpModel(200, 90)
	// Opt-in features show their rows as off: loops (p x L :loop :loops)
	// and turn_snapshots (:undo :redo).
	require.Equal(t, 7, strings.Count(helpText(m), "(off)"), "only the loop and undo rows are off by default")
	require.NoError(t, m.Store.Set("loops", true))
	require.NoError(t, m.Store.Set("turn_snapshots", true))
	require.NotContains(t, helpText(m), "(off)", "every other feature is on by default")

	require.NoError(t, m.Store.Set("undo_complete", false))
	text := helpText(m)
	require.Contains(t, text, "undo done (unmark")
	require.Equal(t, 2, strings.Count(text, "(off)"), "the uc key and the :uc command")
}

func TestHelpFlagsNameRealFeatures(t *testing.T) {
	for _, b := range bindings {
		if b.Flag != "" {
			e, ok := settings.Find(b.Flag)
			require.True(t, ok, "binding %q flag %q", b.Keys, b.Flag)
			require.Equal(t, settings.SectionFeatures, e.Section, b.Flag)
		}
	}
}
