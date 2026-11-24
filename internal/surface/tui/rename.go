package tui

import (
	"fmt"
	"log/slog"
	"strconv"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// RenameState is the in-progress rename of one session's display name.
type RenameState struct {
	SessionID string
	Slug      string
	Orig      string
	// Edit holds the draft name.
	Edit lineEditor
	// Err is why the draft was refused (a name another session holds),
	// shown on the editor line until the next edit.
	Err string
}

// pendingName is the last display name this board sent, so a name_taken
// refusal can reopen the editor with it.
type pendingName struct {
	SessionID string
	Name      string
}

// nameConflict mirrors the daemon's name rule against the board's sessions:
// it returns the refusal for giving session id the display name name, or ""
// when the name is free. Empty names never conflict, and a session may
// always keep (or recase) its own name. The daemon decides; this only saves
// a round trip and keeps the editor open.
func nameConflict(sessions []protocol.SessionSummary, id, name string) string {
	key := protocol.SessionNameKey(name)
	if key == "" {
		return ""
	}
	if own, ok := findSession(sessions, id); ok && protocol.SessionNameKey(own.Name) == key {
		return ""
	}
	for _, s := range sessions {
		if s.ID != id && protocol.SessionNameKey(s.Name) == key {
			return fmt.Sprintf("name %q already in use by %s", s.Name, s.ShortID)
		}
	}
	return ""
}

// sendName sends a display name for id and remembers it for reopenRename.
func (m Model) sendName(id, name string) (Model, tea.Cmd) {
	m.PendingName = &pendingName{SessionID: id, Name: name}
	return m, setNameCmd(namerOf(m), id, name)
}

// reopenRename handles a name_taken Error for a name this board sent
// (request id "rename-<session id>"): the board's rename editor opens again
// with the name and the daemon's message, so the user can fix it. With
// another dialog open, or for a name it did not send, it reports false and
// the caller shows the error on the status line.
func (m Model) reopenRename(ee protocol.ErrorEvent) (Model, bool) {
	p := m.PendingName
	if ee.Code != protocol.ErrCodeNameTaken || p == nil || ee.ID != "rename-"+p.SessionID {
		return m, false
	}
	m.PendingName = nil
	sum, ok := findSession(m.Sessions, p.SessionID)
	if !ok || m.Focus != FocusBoard {
		return m, false
	}
	m.Rename = &RenameState{SessionID: sum.ID, Slug: sum.Slug, Orig: sum.Name,
		Edit: lineEditor{Text: p.Name, Max: protocol.MaxSessionNameRunes}, Err: ee.Message}
	m.Focus = FocusRename
	m.Notice = ""
	m.Err = ""
	slog.Info("tui: rename refused by daemon; editor reopened", "session", sum.ID)
	return m, true
}

// sessionNamer is the narrow client surface rename needs.
type sessionNamer interface {
	SetSessionName(sessionID, name, reqID string) error
}

func openRename(m Model) (Model, tea.Cmd) {
	if m.SelectedID == "" || indexOfSelected(m) < 0 {
		slog.Debug("tui: 'r' pressed with no selection — no-op")
		return m, nil
	}
	sum, ok := findSession(m.Sessions, m.SelectedID)
	if !ok {
		return m, nil
	}
	m.Rename = &RenameState{SessionID: sum.ID, Slug: sum.Slug, Orig: sum.Name, Edit: lineEditor{Text: sum.Name, Max: protocol.MaxSessionNameRunes}}
	m.Focus = FocusRename
	m.Err = ""
	slog.Info("tui: rename opened", "session", sum.ID, "has_name", sum.Name != "")
	return m, nil
}

func updateRenameKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	r := m.Rename
	if r == nil {
		m.Focus = FocusBoard
		return m, nil
	}
	switch k.Type {
	case tea.KeyEsc:
		slog.Info("tui: rename cancelled", "session", r.SessionID)
		m.Rename = nil
		m.Focus = FocusBoard
		return m, nil
	case tea.KeyEnter:
		name, err := protocol.NormalizeSessionName(r.Edit.Text)
		if err == nil && name != r.Orig {
			if msg := nameConflict(m.Sessions, r.SessionID, name); msg != "" {
				// Keep the editor and the draft so the user can fix it.
				r.Err = msg
				slog.Info("tui: rename refused, name taken", "session", r.SessionID)
				return m, nil
			}
		}
		m.Rename = nil
		m.Focus = FocusBoard
		if err != nil {
			m.Err = "rename: " + err.Error()
			return m, nil
		}
		if name == r.Orig {
			return m, nil
		}
		if _, ok := findSession(m.Sessions, r.SessionID); !ok {
			m.Err = "rename: session removed"
			return m, nil
		}
		if name == "" {
			m.Notice = "name cleared — showing slug"
		} else {
			m.Notice = "renamed to " + name
		}
		slog.Info("tui: rename submitted", "session", r.SessionID, "cleared", name == "", "runes", utf8.RuneCountInString(name))
		slog.Debug("tui: rename text", "session", r.SessionID, "name", name)
		return m.sendName(r.SessionID, name)
	default:
		if r.Edit.Edit(k) {
			r.Err = ""
		}
	}
	return m, nil
}

// namerOf returns the model's client as a sessionNamer, or a nil interface
// when there is no client (avoids a typed-nil interface).
func namerOf(m Model) sessionNamer {
	if m.namer != nil {
		return m.namer
	}
	if m.Client == nil {
		return nil
	}
	return m.Client
}

func setNameCmd(c sessionNamer, id, name string) tea.Cmd {
	return func() tea.Msg {
		if c == nil {
			return nil
		}
		if err := c.SetSessionName(id, name, "rename-"+id); err != nil {
			slog.Warn("tui: rename send failed", "session", id, "err", err)
			return CmdErrMsg{Op: "rename", Err: err}
		}
		return nil
	}
}

// renderRenamePrompt is the bottom-line editor: "✎ rename <slug> › draft█ n/64",
// or "… › draft█  ✗ <reason>" after a refused name. The draft is
// tail-truncated so the line never wraps.
func renderRenamePrompt(m Model, width int) string {
	r := m.Rename
	if r == nil {
		return padLine("", width)
	}
	head := styleArrowYellow.Render("✎ rename") + " " + styleDim.Render(truncate(r.Slug, 24)) + styleMuted.Render(" › ")
	count := styleMuted.Render(" " + strconv.Itoa(utf8.RuneCountInString(r.Edit.Text)) + "/" + strconv.Itoa(protocol.MaxSessionNameRunes))
	if r.Err != "" {
		// A refused name: the reason takes the counter's place.
		count = styleStateFailed.Render("  ✗ " + truncate(protocol.FlattenDisplayText(r.Err), max(1, width/2)))
	}
	room := width - 2 - ansi.StringWidth(head) - ansi.StringWidth(count) - 1
	draft := protocol.FlattenDisplayText(r.Edit.Text) // prefilled from the session's current name
	if room < 1 {
		room = 1
	}
	if ansi.StringWidth(draft) > room {
		draft = ansi.TruncateLeft(draft, ansi.StringWidth(draft)-room+1, "…")
	}
	body := stylePrimary.Render(draft) + cursorBlock(m)
	if r.Edit.Text == "" {
		body = cursorBlock(m) + styleDim.Render(truncate("empty = show slug", room))
	}
	return padLine("  "+head+body+count, width)
}
