package tui

import (
	"log/slog"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/features/audio"
)

// chordHandler finishes a board chord. The first key is the boardChords key;
// second is the key just pressed. It reports false when second does not
// complete the chord, so the key is handled normally.
type chordHandler func(m Model, second string) (Model, tea.Cmd, bool)

// boardChords maps a chord's first key to its handler: dd deletes, cc marks
// done (or undoes the mark), z... hides or shows sections.
var boardChords = map[string]chordHandler{
	"d": chordDelete,
	"c": chordDone,
	"z": chordSection,
}

// handleBoardChord runs one board key through the chord table. It reports
// true when the key was consumed (it armed a chord or finished one). A chord
// key leaves Notice alone; any other key clears the pending chord and
// Notice.
func handleBoardChord(m Model, key string) (Model, tea.Cmd, bool) {
	if pending := m.PendingChord; pending != "" {
		m.PendingChord = ""
		if h, ok := boardChords[pending]; ok {
			if m, cmd, done := h(m, key); done {
				return m, cmd, true
			}
		}
	}
	if _, ok := boardChords[key]; ok {
		m.PendingChord = key
		return m, nil, true
	}
	m.Notice = ""
	return m, nil, false
}

// chordDelete is dd: open the delete confirmation for the selection.
func chordDelete(m Model, second string) (Model, tea.Cmd, bool) {
	if second != "d" {
		return m, nil, false
	}
	if m.SelectedID != "" && indexOfSelected(m) >= 0 {
		m.PendingDeleteID = m.SelectedID
		m.PendingPrune, m.PendingPruneKept = nil, 0
		m.Focus = FocusConfirmDelete
		if m.Audio != nil {
			m.Audio.Play(audio.EventOpen)
		}
	}
	return m, nil, true
}

// chordDone is cc, the soft "mark done": the session keeps running, moves to
// Done, and is cleared after retire_after_hours; cc again undoes it.
func chordDone(m Model, second string) (Model, tea.Cmd, bool) {
	if second != "c" {
		return m, nil, false
	}
	if m.SelectedID == "" || indexOfSelected(m) < 0 {
		slog.Debug("tui: 'cc' pressed with no selection — no-op")
		return m, nil, true
	}
	m, cmd := toggleRetire(m, m.SelectedID)
	return m, cmd, true
}
