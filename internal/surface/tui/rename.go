package tui

import (
	"log/slog"
	"strconv"
	"strings"
	"unicode"
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
	Draft     string
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
	m.Rename = &RenameState{SessionID: sum.ID, Slug: sum.Slug, Orig: sum.Name, Draft: sum.Name}
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
		m.Rename = nil
		m.Focus = FocusBoard
		name, err := protocol.NormalizeSessionName(r.Draft)
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
		return m, setNameCmd(namerOf(m), r.SessionID, name)
	case tea.KeyBackspace:
		r.Draft = dropLastRune(r.Draft)
	case tea.KeyCtrlU:
		r.Draft = ""
	case tea.KeyCtrlW:
		r.Draft = dropLastWord(r.Draft)
	case tea.KeySpace:
		r.Draft = appendCapped(r.Draft, " ")
	case tea.KeyRunes:
		r.Draft = appendCapped(r.Draft, typedText(k.Runes))
	}
	return m, nil
}

// appendCapped appends add to s without exceeding MaxSessionNameRunes.
func appendCapped(s, add string) string {
	room := protocol.MaxSessionNameRunes - utf8.RuneCountInString(s)
	if room <= 0 {
		return s
	}
	rs := []rune(add)
	if len(rs) > room {
		rs = rs[:room]
	}
	return s + string(rs)
}

func dropLastWord(s string) string {
	s = strings.TrimRightFunc(s, unicode.IsSpace)
	i := strings.LastIndexFunc(s, unicode.IsSpace)
	if i < 0 {
		return ""
	}
	return s[:i+1]
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

// renderRenamePrompt is the bottom-line editor: "✎ rename <slug> › draft█ n/64".
// The draft is tail-truncated so the line never wraps.
func renderRenamePrompt(m Model, width int) string {
	r := m.Rename
	if r == nil {
		return padLine("", width)
	}
	head := styleArrowYellow.Render("✎ rename") + " " + styleDim.Render(truncate(r.Slug, 24)) + styleMuted.Render(" › ")
	count := styleMuted.Render(" " + strconv.Itoa(utf8.RuneCountInString(r.Draft)) + "/" + strconv.Itoa(protocol.MaxSessionNameRunes))
	room := max(width-2-ansi.StringWidth(head)-ansi.StringWidth(count)-1, 1)
	body := stylePrimary.Render(tailFit(r.Draft, room)) + cursorBlock(m)
	if r.Draft == "" {
		body = cursorBlock(m) + styleDim.Render(truncate("empty = show slug", room))
	}
	return padLine("  "+head+body+count, width)
}
