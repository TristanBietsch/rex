package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

func withSelector(t *testing.T, st *settings.Store) {
	t.Helper()
	t.Cleanup(func() { curSelector = selectorDefaults; curGlyphs = theme.Glyphs{} })
	syncSelector(st)
}

func storeWith(t *testing.T, kv ...any) *settings.Store {
	t.Helper()
	st := settings.NewStore()
	for i := 0; i+1 < len(kv); i += 2 {
		require.NoError(t, st.Set(kv[i].(string), kv[i+1]))
	}
	return st
}

func TestSelectGlyphPrecedence(t *testing.T) {
	withSelector(t, settings.NewStore())
	require.Equal(t, "▸", selectGlyph(), "default")

	curGlyphs = theme.Glyphs{Select: ">"}
	require.Equal(t, ">", selectGlyph(), "theme keeps the theme's own")

	withSelector(t, storeWith(t, "selector_glyph", "☞"))
	curGlyphs = theme.Glyphs{Select: ">"}
	require.Equal(t, "☞", selectGlyph(), "the setting beats the theme")

	withSelector(t, storeWith(t, "selector_glyph", "☞", "ascii_glyphs", true))
	require.Equal(t, ">", selectGlyph(), "ascii_glyphs draws a non-ASCII glyph as >")
}

// A picker's cursor cell is always 2 columns, so it lines up with the
// unselected "  " whatever the glyph's width.
func TestSelectCursorIsTwoColumns(t *testing.T) {
	for _, g := range []string{"▸", "☞", "👉"} {
		withSelector(t, storeWith(t, "selector_glyph", g))
		require.Equal(t, 2, ansi.StringWidth(selectCursor()), g)
	}
}

// Every row's lead is rowIndent columns wide, selected or not, for a 1- and
// a 2-cell glyph and every gap.
func TestRowLeadWidthFollowsSelector(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, g := range []string{"▸", "👉"} {
		for gap := 1; gap <= 3; gap++ {
			st := storeWith(t, "selector_glyph", g, "selector_gap", gap)
			withSelector(t, st)
			m := layoutModel(now, 120, layoutSessions(now)...)
			m.Store = st
			require.Equal(t, 1+ansi.StringWidth(g)+gap, rowIndent())
			for _, s := range m.Sessions {
				indent, _, _ := rowLead(m, s)
				require.Equal(t, rowIndent(), ansi.StringWidth(indent), "%s gap %d row %s", g, gap, s.ID)
				if s.ID == m.SelectedID {
					require.Contains(t, ansi.Strip(indent), g)
				}
			}
		}
	}
}

func blinkModel(t *testing.T, mode string, now time.Time) Model {
	st := storeWith(t, "selector_blink", mode, "selector_blink_ms", 600)
	withSelector(t, st)
	m := layoutModel(now, 120, layoutSessions(now)...)
	m.Store = st
	m.lastInput = now
	return m
}

func TestSelectorBlinkPhases(t *testing.T) {
	start := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	at := func(m Model, d time.Duration) string {
		m.Clock = func() time.Time { return start.Add(d) }
		return selectorMark(m, selectSlotW())
	}

	m := blinkModel(t, "blink", start)
	require.Equal(t, "▸", ansi.Strip(at(m, 0)), "solid right after input")
	require.Equal(t, " ", at(m, 300*time.Millisecond), "off phase keeps the width")
	require.Equal(t, "▸", ansi.Strip(at(m, 600*time.Millisecond)))
	require.Equal(t, "▸", ansi.Strip(at(m, selectorIdle+300*time.Millisecond)), "solid once idle")

	p := blinkModel(t, "pulse", start)
	require.Equal(t, "▸", ansi.Strip(at(p, 300*time.Millisecond)), "pulse never hides the glyph")

	require.NoError(t, m.Store.Set("reduce_motion", true))
	require.Equal(t, "▸", ansi.Strip(at(m, 300*time.Millisecond)), "reduce_motion holds it solid")
}

func TestSelectorBlinkDrivesTheTick(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	m := blinkModel(t, "off", now)
	m.Sessions = nil
	m.Clock = func() time.Time { return now }
	require.False(t, m.wantsSpinnerTick(), "blink off: an idle board rests")

	require.NoError(t, m.Store.Set("selector_blink", "blink"))
	require.True(t, m.wantsSpinnerTick())

	m.Clock = func() time.Time { return now.Add(selectorIdle) }
	require.False(t, m.wantsSpinnerTick(), "no input for a minute: the chain rests")

	m.Clock = func() time.Time { return now }
	m.Focus = FocusPrompt
	require.False(t, m.selectorBlinking(now), "only on the board")
}

// The board renders through View with the setting, so the gutter in a
// screen matches rowIndent.
func TestBoardViewUsesSelectorGap(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	m := layoutModel(now, 120, layoutSessions(now)...)
	m.Store = storeWith(t, "selector_glyph", "☞", "selector_gap", 3, "row_icons", false)
	t.Cleanup(func() { curSelector = selectorDefaults })
	var found bool
	for _, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		found = found || strings.HasPrefix(l, " ☞   ")
	}
	require.True(t, found, "selected row starts with the glyph and a 3-column gap")
}
