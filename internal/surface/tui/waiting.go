package tui

import (
	"fmt"
	"slices"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// waitingSet is one look of the Waiting marker: its frames, one step per
// second, and the still glyph shown under reduce_motion, in the header counts
// and in the legend. A render with SpinnerTick 0 (snapshots, tests) starts on
// the still glyph.
type waitingSet struct {
	Frames []string
	Still  string
}

// waitingSets are the waiting_spinner options. Every frame is one cell, not
// East Asian ambiguous, not an emoji, and not a glyph with a fixed meaning on
// the board (TestWaitingSets_AvoidFixedGlyphs).
var waitingSets = map[string]waitingSet{
	"clock": {Frames: []string{"◴", waitingGlyph, "◶", "◵"}, Still: waitingGlyph},
	// The hourglass turns a full circle: upright, on its side with the sand
	// on the left, upside down, on its side with the sand on the right.
	"hourglass": {Frames: []string{"⧖", "⧑", "⧖", "⧒"}, Still: "⧖"},
	// Sand piles up from the bottom of the cell.
	"sand":     {Frames: []string{"⣀", "⣄", "⣤", "⣦", "⣶", "⣷", "⣿"}, Still: "⣤"},
	"orbit":    {Frames: []string{"◜", "◝", "◞", "◟"}, Still: "◜"},
	"pendulum": {Frames: []string{"⠂", "⡀", "⢀", "⠐", "⢀", "⡀"}, Still: "⡀"},
	"pulse":    {Frames: []string{"∘", "◦", "⊚", "◦"}, Still: "◦"},
}

// waitingFrames is the default (clock) set's frames.
var waitingFrames = waitingSets["clock"].Frames

// waitingTicksPerStep is how many 100 ms spinner ticks make one step.
const waitingTicksPerStep = 10

// waitingSetFor is the set the waiting_spinner setting picks; clock when it
// is unset or unknown.
func waitingSetFor(m Model) waitingSet {
	if m.Store != nil {
		if id, _ := m.Store.Get("waiting_spinner").(string); id != "" {
			if set, ok := waitingSets[id]; ok {
				return set
			}
		}
	}
	return waitingSets["clock"]
}

// waitingStillFor is the still glyph of the active set: the header counts and
// the legend show it.
func waitingStillFor(m Model) string { return waitingSetFor(m).Still }

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
	set := waitingSetFor(m)
	if m.Store != nil {
		if rm, _ := m.Store.Get("reduce_motion").(bool); rm {
			return set.Still
		}
	}
	start := max(slices.Index(set.Frames, set.Still), 0)
	return set.Frames[(start+m.SpinnerTick/waitingTicksPerStep)%len(set.Frames)]
}

// waitingMarkerCell renders the row marker of a waiting session, in the
// working colour (it is busy, not asking for you).
func waitingMarkerCell(m Model, selected bool) string {
	style := styleMarker[protocol.StateWorking].Width(colMarker)
	if selected {
		style = selectedStyle(style)
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
