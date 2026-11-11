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
//	not in Done                       notice: nothing to undo
//	marked done, still running        unmark (Unretire)
//	exited, with a resume id          Resume: respawn in place
//	exited, no resume id              explain: the tool can't be resumed
//
// An exited session keeps its mark until the daemon respawns it (Resume
// clears it), and when it can't be resumed the mark stays: clearing it would
// leave the row in Done anyway. The daemon stays authoritative: a process
// that exits between the key press and the intent is handled in handleResume.
func undoComplete(m Model, id string) (Model, tea.Cmd) {
	sum, ok := findSession(m.Sessions, id)
	if !ok {
		m.Err = "uc: session is gone"
		return m, nil
	}
	name := displayName(sum)
	switch {
	case isTerminalState(sum.State):
		return resumeExited(m, sum, name)
	case !isMarkedDone(sum):
		m.Notice = name + " isn't done"
		return m, nil
	}
	return toggleRetire(m, id) // marked: toggling unmarks
}

// resumeExited sends Resume for an exited session that has a resume id, or
// says why it can't.
func resumeExited(m Model, sum protocol.SessionSummary, name string) (Model, tea.Cmd) {
	rid := resumeIDShown(sum)
	switch {
	case !m.featureOn("resume"):
		m.Err = "uc: " + name + " has exited; resume is off (rex config set resume true)"
		return m, nil
	case rid == "":
		tool := protocol.FlattenDisplayText(sum.ToolID)
		if tool == "" {
			tool = "its tool"
		}
		m.Err = "uc: " + name + " has exited and " + tool + " can't be resumed"
		return m, nil
	}
	r := resumerOf(m)
	if r == nil {
		m.Err = "uc: not connected to the daemon"
		return m, nil
	}
	m.Notice = "resuming " + name + " (resume id " + rid[:colResume] + "…)"
	slog.Info("tui: uc resumes an exited session", "session", sum.ID, "state", sum.State)
	return m, resumeSessionCmd(r, sum.ID)
}

func runUC(m Model, c cmdCall) (Model, tea.Cmd) {
	id, ok := commandTarget(&m, c.Verb, c.Args)
	if !ok {
		return m, nil
	}
	return undoComplete(m, id)
}
