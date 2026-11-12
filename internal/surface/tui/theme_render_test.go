package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/theme"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Every built-in theme renders the board, and each state's marker carries its
// own SGR codes, so no scheme collapses two states into one look.
func TestEveryThemeRendersBoard(t *testing.T) {
	t.Cleanup(func() { applyTheme("default") })
	for _, name := range theme.Names() {
		applyTheme(name)
		withProfile(termenv.TrueColor, func() {
			m := goldenBoard(t, goldenSize{120, 40}, true)
			require.NotPanics(t, func() { _ = m.View() }, name)
			seen := map[string]protocol.State{}
			for _, st := range []protocol.State{protocol.StateWorking, protocol.StateNeedsInput, protocol.StateDone, protocol.StateFailed} {
				cell := stateMarkerCellFor(m, st, 0, false)
				require.True(t, strings.HasPrefix(cell, "\x1b["), "%s: %s marker is styled: %q", name, st, cell)
				sgr := cell[:strings.Index(cell, "m")+1] // termenv joins bold and color into one CSI
				if other, dup := seen[sgr]; dup {
					t.Errorf("%s: %s and %s markers share SGR %q", name, st, other, sgr)
				}
				seen[sgr] = st
			}
		})
	}
}

// The logo takes the accent in every scheme.
func TestLogoAndArrowUseAccent(t *testing.T) {
	t.Cleanup(func() { applyTheme("default") })
	for _, name := range theme.Names() {
		applyTheme(name)
		p, _ := theme.Get(name)
		accent := lipgloss.Color(p.Accent)
		require.Equal(t, accent, styleLogo.GetForeground(), name)
		require.Equal(t, accent, styleArrow.GetForeground(), name)
	}
}

// ansi marks the selection with reverse video instead of a background.
func TestSelectionReverseOnAnsi(t *testing.T) {
	t.Cleanup(func() { applyTheme("default") })
	withProfile(termenv.TrueColor, func() {
		applyTheme("ansi")
		require.True(t, styleSelected.GetReverse())
		require.Contains(t, stateMarkerCellFor(Model{}, protocol.StateDone, 0, true), ";7;")
		applyTheme("default")
		require.False(t, styleSelected.GetReverse())
		require.NotContains(t, stateMarkerCellFor(Model{}, protocol.StateDone, 0, false), ";7;")
	})
}

// Under ansi the dim tones are the terminal's default color drawn faint, not
// color index 8 (the background in Solarized Dark), which would vanish.
func TestAnsiDimTonesAreFaintNotIndex8(t *testing.T) {
	t.Cleanup(func() { applyTheme("default") })
	withProfile(termenv.TrueColor, func() {
		applyTheme("ansi")
		for name, st := range map[string]lipgloss.Style{
			"dim": styleDim, "muted": styleMuted, "border": styleBorderFg, "meta": styleHeaderMeta,
			"chip dim": styleChipDim, "chip sep": styleChipSep, "boot skip": styleBootSkip, "long run 10m": styleLongRun[3],
		} {
			out := st.Render("x")
			require.Contains(t, out, "\x1b[2m", "%s is faint", name)
			require.NotContains(t, out, "38;5;8", name)
			require.NotContains(t, out, "[90m", name)
			require.NotContains(t, out, "38;2", name, "no fixed color")
		}
		applyTheme("default")
		require.NotContains(t, styleDim.Render("x"), "\x1b[2m")
	})
}
