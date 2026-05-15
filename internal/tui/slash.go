package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// slashCmd is a single entry in the slash palette.
type slashCmd struct {
	ID    string // "find"
	Args  string // "<query>"
	Desc  string // "search slug + description"
	Apply func(m Model, args string) (Model, tea.Cmd)
}

// SlashState lives on Model when Focus == FocusSlash.
type SlashState struct {
	Query     string // typed text (without leading "/")
	CursorIdx int    // selected entry index in the filtered list
}

// builtinSlash returns the static built-in slash entries.
func builtinSlash() []slashCmd {
	return []slashCmd{
		{ID: "find", Args: "<query>", Desc: "search slug + description", Apply: applyFind},
		{ID: "new", Args: "", Desc: "open the new-agent wizard", Apply: applyNew},
		{ID: "help", Args: "", Desc: "open the help overlay", Apply: applyHelp},
		{ID: "settings", Args: "", Desc: "open the settings page", Apply: applySettings},
		{ID: "attach", Args: "<sel>", Desc: "open modal on a selector", Apply: applyAttach},
		{ID: "reply", Args: "<sel> <text>", Desc: "reply to a session", Apply: applyReply},
		{ID: "bg", Args: "", Desc: "detach, save place", Apply: applyBg},
		{ID: "reload", Args: "", Desc: "reload tools.yaml via SIGHUP", Apply: applyReload},
		{ID: "q", Args: "", Desc: "quit (confirm)", Apply: applyQuit},
	}
}

func openSlash(m Model) (Model, tea.Cmd) {
	m.Slash = &SlashState{Query: "", CursorIdx: 0}
	m.Focus = FocusSlash
	return m, nil
}

func closeSlash(m Model) Model {
	m.Slash = nil
	m.Focus = FocusBoard
	return m
}

func updateSlashKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	if m.Slash == nil {
		return m, nil
	}
	switch k.Type {
	case tea.KeyEsc:
		return closeSlash(m), nil
	case tea.KeyEnter:
		return executeSlash(m)
	case tea.KeyBackspace:
		if len(m.Slash.Query) > 0 {
			m.Slash.Query = m.Slash.Query[:len(m.Slash.Query)-1]
		}
		m.Slash.CursorIdx = 0
		return m, nil
	case tea.KeyRunes:
		m.Slash.Query += string(k.Runes)
		m.Slash.CursorIdx = 0
		return m, nil
	case tea.KeySpace:
		m.Slash.Query += " "
		m.Slash.CursorIdx = 0
		return m, nil
	}
	switch k.String() {
	case "down", "ctrl+n":
		matches, _ := filterSlash(m.Slash.Query)
		if m.Slash.CursorIdx+1 < len(matches) {
			m.Slash.CursorIdx++
		}
		return m, nil
	case "up", "ctrl+p":
		if m.Slash.CursorIdx > 0 {
			m.Slash.CursorIdx--
		}
		return m, nil
	}
	return m, nil
}

// filterSlash returns built-in commands matching the query (prefix or fuzzy substring).
// Also splits the query into (head, rest) — head matches a command id, rest is args.
func filterSlash(query string) ([]slashCmd, string) {
	q := strings.TrimSpace(query)
	head, rest := q, ""
	if idx := strings.IndexByte(q, ' '); idx >= 0 {
		head = q[:idx]
		rest = strings.TrimSpace(q[idx+1:])
	}
	all := builtinSlash()
	if head == "" {
		return all, ""
	}
	out := make([]slashCmd, 0, len(all))
	for _, s := range all {
		if strings.HasPrefix(s.ID, head) || strings.Contains(s.ID, head) {
			out = append(out, s)
		}
	}
	return out, rest
}

func executeSlash(m Model) (Model, tea.Cmd) {
	query := m.Slash.Query
	matches, rest := filterSlash(query)
	if len(matches) == 0 {
		// Fallback: treat the whole query as a /find argument.
		m.SearchQuery = strings.TrimSpace(query)
		return closeSlash(m), nil
	}
	idx := m.Slash.CursorIdx
	if idx >= len(matches) {
		idx = 0
	}
	chosen := matches[idx]
	m = closeSlash(m)
	updated, cmd := chosen.Apply(m, rest)
	return updated, cmd
}

func renderSlash(m Model) string {
	if m.Slash == nil {
		return ""
	}
	matches, _ := filterSlash(m.Slash.Query)
	var b strings.Builder
	prompt := styleArrow.Render("/") + " " + m.Slash.Query + cursorBlock(m)
	b.WriteString(prompt + "\n\n")
	if len(matches) == 0 {
		b.WriteString(styleDim.Render("no match — enter to /find "+m.Slash.Query) + "\n")
	} else {
		for i, c := range matches {
			cursor := "  "
			if i == m.Slash.CursorIdx {
				cursor = styleArrow.Render("▸ ")
			}
			line := cursor + styleSlug.Render("/"+c.ID)
			if c.Args != "" {
				line += " " + styleDim.Render(c.Args)
			}
			line += "   " + styleDim.Render(c.Desc)
			b.WriteString(line + "\n")
		}
	}
	b.WriteString("\n" + styleDim.Render("j/k or ctrl+n/p select · enter run · esc close · type to filter"))
	return b.String()
}

// --- slash command Apply funcs ---

func applyFind(m Model, args string) (Model, tea.Cmd) {
	m.SearchQuery = strings.TrimSpace(args)
	return m, nil
}

func applyNew(m Model, _ string) (Model, tea.Cmd) {
	return openWizard(m)
}

func applyHelp(m Model, _ string) (Model, tea.Cmd) {
	m.Focus = FocusHelp
	return m, nil
}

func applySettings(m Model, _ string) (Model, tea.Cmd) {
	return openSettings(m)
}

func applyAttach(m Model, args string) (Model, tea.Cmd) {
	id := resolveLocal(m, args)
	if id == "" {
		m.Err = "no match for " + args
		return m, nil
	}
	return openModal(m, id)
}

func applyReply(m Model, args string) (Model, tea.Cmd) {
	parts := strings.SplitN(args, " ", 2)
	if len(parts) != 2 {
		m.Err = "/reply <selector> <text>"
		return m, nil
	}
	id := resolveLocal(m, parts[0])
	if id == "" {
		m.Err = "no match for " + parts[0]
		return m, nil
	}
	text := parts[1]
	c := m.Client
	return m, func() tea.Msg {
		if err := c.Reply(id, text); err != nil {
			return DaemonErrMsg{Err: err}
		}
		return nil
	}
}

func applyBg(m Model, _ string) (Model, tea.Cmd) {
	_ = SaveTUIState(m)
	m.Quitting = true
	return m, tea.Quit
}

func applyReload(m Model, _ string) (Model, tea.Cmd) {
	return m, sendDaemonSIGHUP(m)
}

func applyQuit(m Model, _ string) (Model, tea.Cmd) {
	m.Focus = FocusConfirmQuit
	return m, nil
}

// String helper used by tests / debug.
func (s SlashState) String() string {
	return fmt.Sprintf("Slash(q=%q,idx=%d)", s.Query, s.CursorIdx)
}
