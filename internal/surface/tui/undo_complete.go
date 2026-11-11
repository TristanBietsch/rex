package tui

import (
	"log/slog"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// chordUndoComplete is uc, "undo complete": u then c. A c arriving while u
// is pending completes uc and does not arm cc.
func chordUndoComplete(m Model, second string) (Model, tea.Cmd, bool) {
	if second != "c" {
		return m, nil, false
	}
	if m.SelectedID == "" || indexOfSelected(m) < 0 {
		slog.Debug("tui: 'uc' pressed with no selection — no-op")
		return m, nil, true
	}
	m, cmd := undoComplete(m, m.SelectedID)
	return m, cmd, true
}

// undoComplete takes a session out of Done.
//
//	exited (done, failed or crashed)  explain: it can't be brought back yet
//	not in Done                       notice: nothing to undo
//	marked done, still running        unmark (Unretire)
//
// An exited session keeps its mark: clearing it would leave the row in Done
// anyway.
func undoComplete(m Model, id string) (Model, tea.Cmd) {
	sum, ok := findSession(m.Sessions, id)
	if !ok {
		m.Err = "uc: session is gone"
		return m, nil
	}
	name := displayName(sum)
	switch {
	case sum.State == protocol.StateDone || sum.State == protocol.StateFailed || sum.State == protocol.StateCrashed:
		m.Err = "uc: " + name + " has exited; it can't be brought back yet"
		return m, nil
	case !isMarkedDone(sum):
		m.Notice = name + " isn't done"
		return m, nil
	}
	return toggleRetire(m, id) // marked: toggling unmarks
}

func runUC(m Model, c cmdCall) (Model, tea.Cmd) {
	id, ok := commandTarget(&m, c.Verb, c.Args)
	if !ok {
		return m, nil
	}
	return undoComplete(m, id)
}
