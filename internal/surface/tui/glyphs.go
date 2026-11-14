package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// curGlyphs are the active theme's marker overrides (theme.Palette.Glyphs).
// applyPalette writes it on the UI goroutine; an empty field means the
// built-in marker.
var curGlyphs theme.Glyphs

// selectDefault is the selection arrow when the theme does not set one.
const selectDefault = "▸"

// The built-in state markers. Every one is a single cell, East Asian narrow
// or neutral (never ambiguous), has no emoji presentation, and is drawn by
// Menlo itself (so by every macOS terminal font that falls back to it):
//
//   - readyGlyph: a raised flag, "your turn". Ready is green.
//   - doneGlyph: a check mark. Done is a calmer green than Ready, so the
//     two greens are told apart by shape as well as by shade.
//   - failedGlyph: the ballot X that pairs with the check mark.
//   - crashedGlyph: a slashed circle, "stopped dead".
const (
	readyGlyph   = "⚑"
	doneGlyph    = "✓"
	failedGlyph  = "✗"
	crashedGlyph = "⊘"
)

// stateGlyph is the marker character for a state: the theme's override, else
// the built-in one. Working has no fixed glyph (the spinner), so it and any
// state without a marker return "".
func stateGlyph(st protocol.State) string {
	pick := func(override, def string) string {
		if override != "" {
			return override
		}
		return def
	}
	switch st {
	case protocol.StateNeedsInput:
		return pick(curGlyphs.Needs, readyGlyph)
	case protocol.StateDone:
		return pick(curGlyphs.Done, doneGlyph)
	case protocol.StateFailed:
		return pick(curGlyphs.Failed, failedGlyph)
	case protocol.StateCrashed:
		return pick(curGlyphs.Crashed, crashedGlyph)
	case protocol.StateQueued:
		return pick(curGlyphs.Queued, queuedGlyph)
	case protocol.StateWorking:
		return ""
	}
	return ""
}

// selectorCfg is the selector_* settings the renderer reads. syncSelector
// writes curSelector from the Store at the top of View, on the UI goroutine,
// so the many pickers that have no Model at hand read one consistent value.
type selectorCfg struct {
	glyph   string // selector_glyph; "" or theme keeps the theme's own
	ascii   bool   // ascii_glyphs: a non-ASCII glyph draws as ">"
	bold    bool
	gap     int // columns between the board selector and the state marker
	blink   string
	blinkMS int
}

// selectorDefaults matches the registry defaults, for a Model without a
// Store (tests, rex render).
var selectorDefaults = selectorCfg{bold: true, gap: 2, blink: "off", blinkMS: 600}

var curSelector = selectorDefaults

// syncSelector loads curSelector from the settings.
func syncSelector(st *settings.Store) {
	c := selectorDefaults
	if st != nil {
		c.glyph = st.String("selector_glyph")
		c.ascii, _ = st.Get("ascii_glyphs").(bool)
		c.bold, _ = st.Get("selector_bold").(bool)
		c.blink = st.String("selector_blink")
		if g, ok := st.Get("selector_gap").(int); ok {
			c.gap = g
		}
		if ms, ok := st.Get("selector_blink_ms").(int); ok {
			c.blinkMS = ms
		}
	}
	curSelector = c
}

// selectGlyph is the arrow that marks the selected row and wizard cursor:
// the selector_glyph setting, else the theme's, else selectDefault.
func selectGlyph() string {
	g := curSelector.glyph
	if g == "" || g == settings.SelectorTheme {
		g = selectDefault
		if curGlyphs.Select != "" {
			g = curGlyphs.Select
		}
	}
	if curSelector.ascii && !isASCII(g) {
		return ">"
	}
	return g
}

// selectSlotW is the cell width of selectGlyph (1 or 2).
func selectSlotW() int {
	return max(1, ansi.StringWidth(selectGlyph()))
}

// selectArrow is selectGlyph in the selector's style.
func selectArrow() string {
	return styleArrow.Bold(curSelector.bold).Render(selectGlyph())
}

// selectCursor is a picker's 2-column cursor cell: the arrow and a space,
// or a 2-cell glyph alone, so rows line up with the unselected "  ".
func selectCursor() string {
	return selectArrow() + strings.Repeat(" ", max(0, 2-selectSlotW()))
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// loopDefault is the marker of a loop session's row when the theme does not
// set one.
const loopDefault = "↻"

// loopGlyph marks a loop session's row and the loops overlay.
func loopGlyph() string {
	if curGlyphs.Loop != "" {
		return curGlyphs.Loop
	}
	return loopDefault
}
