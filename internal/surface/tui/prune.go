package tui

import (
	"fmt"
	"log/slog"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/features/audio"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// PruneTargets splits the board's Completed column for a prune (TUI :prune,
// CLI rex prune). targets are the sessions to delete. kept counts Completed
// sessions left alone because their agent is still working or queued: they
// were marked done while busy, and the daemon clears them once they settle.
// Sessions in other columns are never targets.
func PruneTargets(sessions []protocol.SessionSummary) (targets []protocol.SessionSummary, kept int) {
	for _, s := range sessions {
		switch {
		case Prunable(s):
			targets = append(targets, s)
		case inCompleted(s):
			kept++
		}
	}
	return targets, kept
}

// Prunable reports whether a prune may delete s right now: it is on the
// Completed column and its agent is not working or queued. Callers check it
// again just before deleting, because a marked-done session goes back to
// work when someone types into it.
func Prunable(s protocol.SessionSummary) bool {
	return inCompleted(s) && s.State != protocol.StateWorking && s.State != protocol.StateQueued
}

// stillPrunable keeps the ids that are still prunable in sessions (the live
// board); a session that changed or went away since the prompt opened drops.
func stillPrunable(sessions []protocol.SessionSummary, ids []string) []string {
	byID := make(map[string]protocol.SessionSummary, len(sessions))
	for _, s := range sessions {
		byID[s.ID] = s
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if s, ok := byID[id]; ok && Prunable(s) {
			out = append(out, id)
		}
	}
	return out
}

// PruneKeptNote is the "(N still working kept)" suffix, or "" when none.
func PruneKeptNote(kept int) string {
	if kept == 0 {
		return ""
	}
	return fmt.Sprintf(" (%d still working kept)", kept)
}

// sessionDeleter is the narrow client surface prune needs (tests fake it).
type sessionDeleter interface {
	Delete(sessionID string) error
}

func deleterOf(m Model) sessionDeleter {
	if m.deleter != nil {
		return m.deleter
	}
	if m.Client == nil {
		return nil
	}
	return m.Client
}

// openPrune handles :prune. It asks before deleting, reusing the delete
// confirmation prompt with the whole target list.
func openPrune(m Model) (Model, tea.Cmd) {
	targets, kept := PruneTargets(m.Sessions)
	if len(targets) == 0 {
		m.Notice = "nothing to clear" + PruneKeptNote(kept)
		return m, nil
	}
	ids := make([]string, len(targets))
	for i, s := range targets {
		ids[i] = s.ID
	}
	m.PendingPrune, m.PendingPruneKept = ids, kept
	m.PendingDeleteID = ""
	m.Focus = FocusConfirmDelete
	if m.Audio != nil {
		m.Audio.Play(audio.EventOpen)
	}
	return m, nil
}

// pruneDoneMsg reports how many Delete intents went out. The TUI does not
// wait for acks: the board drops each row as the daemon removes it.
type pruneDoneMsg struct {
	sent, failed int
	kept         int // still working when the prompt opened
	skipped      int // changed after the prompt opened
	err          error
}

// confirmPrune runs on y: it deletes the pending sessions that are still
// prunable on the live board.
func confirmPrune(m Model) (Model, tea.Cmd) {
	ids := stillPrunable(m.Sessions, m.PendingPrune)
	skipped := len(m.PendingPrune) - len(ids)
	kept := m.PendingPruneKept
	m.PendingPrune, m.PendingPruneKept = nil, 0
	m.Focus = FocusBoard
	d := deleterOf(m)
	if d == nil {
		m.Err = "prune: not connected to the daemon"
		return m, nil
	}
	if len(ids) == 0 {
		m.Notice = "nothing to clear: every session changed since you asked"
		return m, nil
	}
	if m.Audio != nil {
		m.Audio.Play(audio.EventDelete)
	}
	return m, pruneCmd(d, ids, kept, skipped)
}

func pruneCmd(d sessionDeleter, ids []string, kept, skipped int) tea.Cmd {
	return func() tea.Msg {
		msg := pruneDoneMsg{kept: kept, skipped: skipped}
		for _, id := range ids {
			if err := d.Delete(id); err != nil {
				msg.failed++
				if msg.err == nil {
					msg.err = err
				}
				continue
			}
			msg.sent++
		}
		slog.Info("tui: prune", "sent", msg.sent, "failed", msg.failed, "kept", kept, "skipped", skipped)
		return msg
	}
}

func applyPruneDone(m Model, msg pruneDoneMsg) Model {
	if msg.failed > 0 {
		m.Err = fmt.Sprintf("prune: %d of %d failed: %v", msg.failed, msg.sent+msg.failed, msg.err)
		return m
	}
	m.Notice = fmt.Sprintf("deleting %d", msg.sent) + PruneKeptNote(msg.kept) + pruneSkippedNote(msg.skipped)
	return m
}

// pruneSkippedNote is the "; N changed since you asked, kept" suffix.
func pruneSkippedNote(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("; %d changed since you asked, kept", n)
}
