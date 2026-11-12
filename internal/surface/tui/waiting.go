package tui

import (
	"fmt"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// waitingFrames is the Waiting marker's animation: one step per second
// while a session waits on a tool. waitingGlyph (◷) is frame 1, so a render
// with SpinnerTick 0 (snapshots, tests) shows the still glyph.
var waitingFrames = []string{"◴", waitingGlyph, "◶", "◵"}

// waitingTicksPerStep is how many 100 ms spinner ticks make one step.
const waitingTicksPerStep = 10

// waitingKindOf is what the session is waiting on in the model's view: ""
// unless the waiting_section feature is on and the session is in Waiting
// (working on a tool call for at least protocol.WaitingAfter).
func waitingKindOf(m Model, s protocol.SessionSummary) string {
	if !m.featureOn("waiting_section") {
		return ""
	}
	return s.WaitingFor(m.now())
}

// waitingGlyphFor is the marker glyph now: the turning frame, or the still
// glyph under reduce_motion.
func waitingGlyphFor(m Model) string {
	if m.Store != nil {
		if rm, _ := m.Store.Get("reduce_motion").(bool); rm {
			return waitingGlyph
		}
	}
	return waitingFrames[(1+m.SpinnerTick/waitingTicksPerStep)%len(waitingFrames)]
}

// waitingMarkerCell renders the row marker of a waiting session, in the
// working colour (it is busy, not asking for you).
func waitingMarkerCell(m Model, selected bool) string {
	style := styleMarker[protocol.StateWorking].Width(colMarker)
	if selected {
		style = style.Background(colorBgSel)
	}
	return style.Render(waitingGlyphFor(m))
}

// waitingLabel is the detail text of a waiting session: the reason and how
// long it has waited, "shell 42s".
func waitingLabel(m Model, s protocol.SessionSummary) string {
	return fmt.Sprintf("%s %s", s.WaitingOn, durationAgoAt(m.now(), s.WaitingSince))
}

// waitingChip is the attach header chip of a waiting session.
func waitingChip(m Model, s protocol.SessionSummary) string {
	return styleMarker[protocol.StateWorking].
		Render(waitingGlyphFor(m) + " waiting (" + s.WaitingOn + ")")
}
