package tui

import (
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

// selectGlyph is the arrow that marks the selected row and wizard cursor.
func selectGlyph() string {
	if curGlyphs.Select != "" {
		return curGlyphs.Select
	}
	return selectDefault
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
