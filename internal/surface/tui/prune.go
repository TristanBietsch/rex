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
		if !inCompleted(s) {
			continue
		}
		if s.State == protocol.StateWorking || s.State == protocol.StateQueued {
			kept++
			continue
		}
		targets = append(targets, s)
	}
	return targets, kept
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

// pruneDoneMsg reports how many Delete intents went out.
type pruneDoneMsg struct {
	sent, failed int
	kept         int
	err          error
}

func pruneCmd(d sessionDeleter, ids []string, kept int) tea.Cmd {
	if d == nil {
		return nil
	}
	return func() tea.Msg {
		msg := pruneDoneMsg{kept: kept}
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
		slog.Info("tui: prune", "sent", msg.sent, "failed", msg.failed, "kept", kept)
		return msg
	}
}

func applyPruneDone(m Model, msg pruneDoneMsg) Model {
	if msg.failed > 0 {
		m.Err = fmt.Sprintf("prune: %d of %d failed: %v", msg.failed, msg.sent+msg.failed, msg.err)
		return m
	}
	m.Notice = fmt.Sprintf("cleared %d", msg.sent) + PruneKeptNote(msg.kept)
	return m
}
