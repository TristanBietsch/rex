package tui

import (
	"log/slog"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// colResume is the resume id column: the first 8 characters of the UUID.
const colResume = 8

// sessionResumer is the narrow client surface the Resume intent needs.
// *client.Client satisfies it; tests inject a recorder.
type sessionResumer interface {
	Resume(sessionID, reqID string) error
}

// resumerOf mirrors retirerOf for the Resume intent.
func resumerOf(m Model) sessionResumer {
	if m.resumer != nil {
		return m.resumer
	}
	if m.Client == nil {
		return nil
	}
	return m.Client
}

// resumeSessionCmd sends Resume for id. The request id carries the session
// so friendlyDaemonError can tell a rejected resume from other errors.
func resumeSessionCmd(r sessionResumer, id string) tea.Cmd {
	return func() tea.Msg {
		if err := r.Resume(id, "resume-"+id); err != nil {
			slog.Error("tui: resume intent failed", "session", id, "err", err)
			return CmdErrMsg{Op: "resume", Err: err}
		}
		return nil
	}
}

// resumeIDShown is the id the row may display: a valid resume id, else "".
// An id from an older or hand-edited daemon file is never shown.
func resumeIDShown(s protocol.SessionSummary) string {
	if protocol.ValidResumeID(s.ResumeID) {
		return s.ResumeID
	}
	return ""
}

// resumeCell renders the resume id prefix. Rows in Done show it in full
// colour (it is what uc resumes); live rows keep it dim.
func resumeCell(s protocol.SessionSummary, w int) string {
	style := lipgloss.NewStyle().Foreground(colorFgDim)
	if inCompleted(s) {
		style = lipgloss.NewStyle().Foreground(colorFgPrimary)
	}
	id := resumeIDShown(s)
	if len(id) > colResume {
		id = id[:colResume]
	}
	return cell(style, id, w)
}

// runRID is `:rid [sel]`: put the full resume id on the status line, so it
// can be selected with the terminal's own selection (Option/Shift-drag).
func runRID(m Model, c cmdCall) (Model, tea.Cmd) {
	id, ok := commandTarget(&m, c.Verb, c.Args)
	if !ok {
		return m, nil
	}
	sum, found := findSession(m.Sessions, id)
	if !found {
		m.Err = "rid: session is gone"
		return m, nil
	}
	rid := resumeIDShown(sum)
	if rid == "" {
		m.Err = "rid: " + displayName(sum) + " has no resume id"
		return m, nil
	}
	m.Notice = displayName(sum) + ": resume id " + rid
	return m, nil
}
