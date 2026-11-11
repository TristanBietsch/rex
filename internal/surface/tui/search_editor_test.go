package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// searchModel has auth rows in each section, a billing row, selection on
// the first row.
func searchModel() Model {
	return boardModel(
		protocol.SessionSummary{ID: "a1", Slug: "auth-fix", ToolID: "claude", State: protocol.StateNeedsInput},
		protocol.SessionSummary{ID: "b1", Slug: "billing", ToolID: "claude", State: protocol.StateWorking},
		protocol.SessionSummary{ID: "a2", Slug: "auth-docs", ToolID: "claude", State: protocol.StateWorking},
		protocol.SessionSummary{ID: "d1", Slug: "done-auth", ToolID: "claude", State: protocol.StateDone},
	)
}

func typeKeys(m Model, keys ...string) Model {
	for _, k := range keys {
		m, _ = updateKey(m, key(k))
	}
	return m
}

func rowIDs(m Model) []string {
	var ids []string
	for _, s := range orderedSessions(m) {
		ids = append(ids, s.ID)
	}
	return ids
}

func TestSearchEditorLive(t *testing.T) {
	m := searchModel()
	m = typeKeys(m, "/")
	require.Equal(t, FocusSearch, m.Focus)
	require.ElementsMatch(t, []string{"a1", "b1", "a2", "d1"}, rowIDs(m))

	m = typeKeys(m, "a", "u", "t", "h")
	require.Equal(t, "auth", m.Search)
	require.ElementsMatch(t, []string{"a1", "a2", "d1"}, rowIDs(m), "narrows as you type")

	m = typeKeys(m, " ", "d", "o", "c", "s")
	require.Equal(t, []string{"a2"}, rowIDs(m))

	m = typeKeys(m, "ctrl+w")
	require.Equal(t, "auth ", m.Search)
	m = typeKeys(m, "backspace")
	require.Equal(t, "auth", m.Search)
	m = typeKeys(m, "ctrl+u")
	require.Equal(t, "", m.Search)
	require.Len(t, rowIDs(m), 4)

	m = typeKeys(m, "b", "i", "l", "enter")
	require.Equal(t, FocusBoard, m.Focus, "enter returns to the board")
	require.Equal(t, "bil", m.Search, "and keeps the query")
	require.Equal(t, []string{"b1"}, rowIDs(m))
}

func TestSearchEscRestoresPrevious(t *testing.T) {
	m := searchModel()
	m = typeKeys(m, "/", "a", "u", "t", "h", "enter")
	require.Equal(t, "auth", m.Search)

	m = typeKeys(m, "/")
	require.Equal(t, FocusSearch, m.Focus)
	require.Equal(t, "auth", m.Search, "reopens prefilled")
	m = typeKeys(m, " ", "d", "o", "c")
	require.Equal(t, []string{"a2"}, rowIDs(m))
	m = typeKeys(m, "esc")
	require.Equal(t, FocusBoard, m.Focus)
	require.Equal(t, "auth", m.Search, "esc restores the previous query")
	require.ElementsMatch(t, []string{"a1", "a2", "d1"}, rowIDs(m))
}

func TestSearchBoardEscClears(t *testing.T) {
	m := searchModel()
	m = typeKeys(m, "/", "b", "i", "l", "enter")
	require.Equal(t, []string{"b1"}, rowIDs(m))
	m = typeKeys(m, "esc")
	require.Equal(t, "", m.Search)
	require.Len(t, rowIDs(m), 4)

	// With no search, esc stays a no-op.
	m.SelectedID = "b1"
	m = typeKeys(m, "esc")
	require.Equal(t, "b1", m.SelectedID)
	require.Equal(t, FocusBoard, m.Focus)
}

func TestSearchEmptyDraftIsOff(t *testing.T) {
	m := searchModel()
	m = typeKeys(m, "/", " ", " ", "enter")
	require.Equal(t, "", m.Search)
}

func TestSearchSelection(t *testing.T) {
	m := searchModel()
	m.SelectedID = "a2"
	m = typeKeys(m, "/", "a", "u", "t", "h")
	require.Equal(t, "a2", m.SelectedID, "selection stays while it is still visible")

	m = typeKeys(m, "-", "f", "i", "x")
	require.Equal(t, "a1", m.SelectedID, "else moves to the first match")

	m = typeKeys(m, "z")
	require.Equal(t, "", m.SelectedID, "no matches leaves nothing selected")
	require.Empty(t, rowIDs(m))
}

func TestSearchInputSanitized(t *testing.T) {
	m := searchModel()
	m = typeKeys(m, "/")
	m, _ = updateKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a\x1b[2Ju\nth\x00")})
	require.Equal(t, "a[2Juth", m.Search, "control runes dropped")

	m.Search = ""
	m, _ = updateKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.Repeat("x", 300))})
	require.Equal(t, maxSearchRunes, utf8.RuneCountInString(m.Search))
	m = typeKeys(m, "y", " ")
	require.Equal(t, maxSearchRunes, utf8.RuneCountInString(m.Search), "cap holds for later keys")
}

func TestSearchCursorBlinks(t *testing.T) {
	m := searchModel()
	m = typeKeys(m, "/")
	require.True(t, m.cursorBlinking())
	require.True(t, m.wantsSpinnerTick())
}

func TestSearchPromptAndHelpLine(t *testing.T) {
	m := searchModel()
	m = typeKeys(m, "/", "a", "u", "t", "h")
	line := ansi.Strip(renderPrompt(m, 100))
	require.Contains(t, line, "/ auth")
	require.Contains(t, line, "3 matches")
	require.Equal(t, 100, ansi.StringWidth(line))

	help := ansi.Strip(renderHelpLine(m, 100))
	require.Contains(t, help, "enter keep")
	require.Contains(t, help, "esc cancel")
	require.Contains(t, help, "ctrl+u clear")
	require.Contains(t, help, "ctrl+w delete word")

	m = typeKeys(m, "z")
	require.Contains(t, ansi.Strip(renderPrompt(m, 100)), "no matches")
}

func TestSearchPromptTruncatesLongQuery(t *testing.T) {
	m := searchModel()
	m = typeKeys(m, "/")
	m, _ = updateKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(strings.Repeat("w", 120))})
	line := ansi.Strip(renderPrompt(m, 40))
	require.Equal(t, 40, ansi.StringWidth(line))
	require.NotContains(t, line, "\n")
}

func TestSearchViewHeight(t *testing.T) {
	m := searchModel()
	m = typeKeys(m, "/", "a", "u", "t", "h")
	out := m.View()
	require.Len(t, strings.Split(out, "\n"), m.Height)
	plain := ansi.Strip(out)
	require.Contains(t, plain, "auth-fix")
	require.NotContains(t, plain, "billing")
	require.Contains(t, plain, "/ auth")
}
