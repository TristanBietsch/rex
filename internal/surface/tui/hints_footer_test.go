package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// A long Done column puts its fold keys at the end of the help line, after
// the five board keys.
func TestHelpLineCarriesDoneFoldHints(t *testing.T) {
	m := simpleBoard(doneSessions(doneCollapseAt + 1)...)
	line := strings.TrimSpace(ansi.Strip(renderHelpLine(m, 200)))
	require.Equal(t, "enter open · i new · cc done · tab next · ? more · 4 show · :prune clear", line)

	short := simpleBoard(doneSessions(doneCollapseAt)...)
	require.NotContains(t, ansi.Strip(renderHelpLine(short, 200)), ":prune", "a short Done column has no fold keys")
}

// The fold keys give way before the board keys on a narrow line, and the
// line never wraps or overflows.
func TestHelpLineDoneHintsDropWhenNarrow(t *testing.T) {
	m := simpleBoard(doneSessions(doneCollapseAt + 1)...)
	require.Equal(t, 30, ansi.StringWidth(renderHelpLine(m, 30)))
	for _, w := range []int{52, 58, 60, 73, 74, 76} {
		line := renderHelpLine(m, w)
		require.Equal(t, w, ansi.StringWidth(line), "width %d", w)
		plain := ansi.Strip(line)
		require.NotContains(t, plain, "…", "width %d: hints drop whole, never cut", w)
		require.Contains(t, plain, "? more", "width %d: the board keys stay", w)
		if strings.Contains(plain, ":prune") {
			require.Contains(t, plain, "4 show", "width %d: show/hide outranks prune", w)
		}
	}
	require.NotContains(t, ansi.Strip(renderHelpLine(m, 58)), "4 show")
	require.Contains(t, ansi.Strip(renderHelpLine(m, 60)), "4 show")
	require.NotContains(t, ansi.Strip(renderHelpLine(m, 73)), ":prune")
	require.Contains(t, ansi.Strip(renderHelpLine(m, 74)), ":prune clear")
}

// A hidden Done section (z chords) has no fold keys to offer.
func TestHelpLineNoDoneHintsWhenDoneHidden(t *testing.T) {
	m := simpleBoard(doneSessions(doneCollapseAt + 1)...)
	m.Hidden = map[string]bool{"done": true}
	require.NotContains(t, ansi.Strip(renderHelpLine(m, 200)), ":prune")
}

// show_help_bar off blanks the key hints but keeps the row, and still shows
// errors and notices: they are status, not hints.
func TestHelpBarOffHidesHintsOnly(t *testing.T) {
	m := simpleBoard(doneSessions(doneCollapseAt + 1)...)
	m.Store = settings.NewStore()
	require.NoError(t, m.Store.Set("show_help_bar", false))
	line := renderHelpLine(m, 80)
	require.Equal(t, 80, ansi.StringWidth(line), "the row stays")
	require.Empty(t, strings.TrimSpace(ansi.Strip(line)))

	m.Notice = "nothing ready"
	require.Contains(t, ansi.Strip(renderHelpLine(m, 80)), "nothing ready")
	m.Notice, m.Err = "", "boom"
	require.Contains(t, ansi.Strip(renderHelpLine(m, 80)), "err: boom")

	full := ansi.Strip(renderFullScreen(simpleBoardStore(t, false), 80, 24))
	require.NotContains(t, full, "? more")
	require.Contains(t, ansi.Strip(renderFullScreen(simpleBoardStore(t, true), 80, 24)), "? more")
}

func simpleBoardStore(t *testing.T, show bool) Model {
	t.Helper()
	m := simpleBoard(protocol.SessionSummary{ID: "a", Slug: "a", State: protocol.StateWorking})
	m.Store = settings.NewStore()
	require.NoError(t, m.Store.Set("show_help_bar", show))
	return m
}
