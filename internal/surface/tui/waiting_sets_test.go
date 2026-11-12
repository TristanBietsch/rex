package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// waitingSetModel is a waiting board with the waiting_spinner setting on id.
func waitingSetModel(t *testing.T, id string, reduce bool) Model {
	t.Helper()
	st := settings.NewStore()
	require.NoError(t, st.Set("waiting_spinner", id))
	require.NoError(t, st.Set("reduce_motion", reduce))
	m := waitingBoard()
	m.Store = st
	return m
}

// The setting's options are exactly the frame sets, and the default is
// the clock the board has always shown.
func TestWaitingSets_MatchSetting(t *testing.T) {
	s, ok := settings.Find("waiting_spinner")
	require.True(t, ok)
	require.Equal(t, "Waiting icon", s.Label)
	require.Equal(t, settings.SectionAppearance, s.Section)
	require.Equal(t, "clock", s.Default)
	require.Equal(t, []string{"clock", "hourglass", "sand", "orbit", "pendulum", "pulse"}, s.Options)
	require.Len(t, waitingSets, len(s.Options))
	for _, id := range s.Options {
		_, ok := waitingSets[id]
		require.True(t, ok, id)
	}
	require.Equal(t, waitingSet{Frames: []string{"◴", "◷", "◶", "◵"}, Still: "◷"}, waitingSets["clock"])
	// The hourglass turns a full circle: upright, on its side with the sand
	// to the left, upside down, on its side with the sand to the right.
	require.Equal(t, waitingSet{Frames: []string{"⧖", "⧑", "⧖", "⧒"}, Still: "⧖"}, waitingSets["hourglass"])
}

// The marker column is one cell: every frame is one cell wide in every width
// helper the board uses, also with ambiguous widths read as wide (so it is not
// East Asian ambiguous) and in uniseg (so it is not an emoji presentation).
func TestWaitingSets_WidthOne(t *testing.T) {
	wide := runewidth.NewCondition()
	wide.EastAsianWidth = true
	narrow := runewidth.NewCondition()
	narrow.EastAsianWidth = false
	for id, set := range waitingSets {
		require.GreaterOrEqual(t, len(set.Frames), 2, id)
		require.Contains(t, set.Frames, set.Still, "%s: the still glyph is one of the frames", id)
		for _, g := range set.Frames {
			require.Equal(t, 1, ansi.StringWidth(g), "%s %q", id, g)
			require.Equal(t, 1, lipgloss.Width(g), "%s %q", id, g)
			require.Equal(t, 1, lipglossWidth(g), "%s %q", id, g)
			require.Equal(t, 1, uniseg.StringWidth(g), "%s %q emoji?", id, g)
			require.Equal(t, 1, narrow.StringWidth(g), "%s %q", id, g)
			require.Equal(t, 1, wide.StringWidth(g), "%s %q is ambiguous width", id, g)
			require.Len(t, []rune(g), 1, "%s %q", id, g)
		}
	}
}

// No frame is a glyph that already means something on the board: a state
// marker, the selection arrow, the loop mark, the subagent and long-run
// badges, a working spinner frame, an agent icon, or R18's row-strip glyphs
// (phase 2 included). No two sets share a frame.
func TestWaitingSets_AvoidFixedGlyphs(t *testing.T) {
	taken := map[string]string{}
	for _, g := range []string{"◆", "◌", "●", "✕", "○", "▍", "▴", "⋔", "⟳", "✦", "✓", "$", "?", "▸", "↯", "⧗", "*",
		selectDefault, loopDefault, queuedGlyph, subagentGlyph} {
		taken[g] = "board glyph"
	}
	for _, st := range []protocol.State{protocol.StateNeedsInput, protocol.StateDone, protocol.StateFailed, protocol.StateCrashed, protocol.StateQueued} {
		taken[stateGlyph(st)] = "state marker"
	}
	for id, frames := range spinnerFrameSets {
		for _, f := range frames {
			taken[f] = "spinner " + id
		}
	}
	reg, err := registry.Load("")
	require.NoError(t, err)
	for _, tool := range reg.Tools {
		if tool.Icon != "" {
			taken[tool.Icon] = "icon of " + tool.ID
		}
	}
	owner := map[string]string{}
	for id, set := range waitingSets {
		for _, g := range set.Frames {
			require.Empty(t, taken[g], "%s frame %q is a %s", id, g, taken[g])
			if o, ok := owner[g]; ok && o != id {
				t.Fatalf("%q is in both %s and %s", g, o, id)
			}
			owner[g] = id
		}
	}
}

// Every set starts on its still glyph at tick 0 (snapshots), steps once a
// second through its frames, and stays still under reduce_motion. The row,
// the attach chip, the header and the legend all follow the setting.
func TestWaitingSets_Animate(t *testing.T) {
	for id, set := range waitingSets {
		m := waitingSetModel(t, id, false)
		start := 0
		for i, f := range set.Frames {
			if f == set.Still {
				start = i
				break
			}
		}
		for step := range 2 * len(set.Frames) {
			for tick := range waitingTicksPerStep {
				m.SpinnerTick = step*waitingTicksPerStep + tick
				require.Equal(t, set.Frames[(start+step)%len(set.Frames)], waitingGlyphFor(m), "%s step %d tick %d", id, step, tick)
			}
		}
		rm := waitingSetModel(t, id, true)
		for _, tick := range []int{0, 10, 20, 77} {
			rm.SpinnerTick = tick
			require.Equal(t, set.Still, waitingGlyphFor(rm), id)
		}
	}
}

func TestWaitingSets_Wired(t *testing.T) {
	m := waitingSetModel(t, "hourglass", false)
	m.SpinnerTick = waitingTicksPerStep // ⧑
	m.Detailed = true
	require.Contains(t, ansi.Strip(renderBoard(m, 120, 0)), "⧑", "row marker")
	m.Attach = &AttachState{SessionID: "w1", Slug: "w1"}
	require.Contains(t, ansi.Strip(attachTitleBar(m, m.Attach, 100)), "⧑ waiting (shell)", "attach chip")

	var legend []string
	for _, r := range helpLegend(m).Rows {
		legend = append(legend, ansi.Strip(r.Key))
	}
	require.Contains(t, legend, "⧖", "legend shows the still glyph")
	require.NotContains(t, legend, waitingGlyph)

	require.NoError(t, m.Store.Set("header_style", "glyphs"))
	require.Contains(t, ansi.Strip(renderHeader(m, 200)), "◆ 1  ⧖ 2  ⟳ 2  ● 1", "header counts")
}
