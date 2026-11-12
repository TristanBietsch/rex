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
		return pick(curGlyphs.Needs, "◆")
	case protocol.StateDone:
		return pick(curGlyphs.Done, "●")
	case protocol.StateFailed:
		return pick(curGlyphs.Failed, "✕")
	case protocol.StateCrashed:
		return pick(curGlyphs.Crashed, "○")
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
const loopDefault = "⟳"

// loopGlyph marks a loop session's row and the loops overlay.
func loopGlyph() string {
	if curGlyphs.Loop != "" {
		return curGlyphs.Loop
	}
	return loopDefault
}
