package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSearchCommand(t *testing.T) {
	m, _ := executeCommand(searchModel(), "search auth docs")
	require.Equal(t, "auth docs", m.Search, "the rest of the line, like :name")
	require.Empty(t, m.Err)
	require.Equal(t, []string{"a2"}, rowIDs(m))
	require.Equal(t, FocusBoard, m.Focus)
	require.Equal(t, "a2", m.SelectedID, "selection moves to the first match")

	m, _ = executeCommand(m, "search")
	require.Equal(t, "", m.Search, "no argument clears")
	require.Len(t, rowIDs(m), 4)
}

func TestSearchCommandKeepsInnerSpacing(t *testing.T) {
	m, _ := executeCommand(searchModel(), "search   auth   fix  ")
	require.Equal(t, "auth   fix", m.Search)
	require.Equal(t, []string{"a1"}, rowIDs(m))
}

func TestSearchCommandSanitizes(t *testing.T) {
	m, _ := executeCommand(searchModel(), "search a\x1b[31mu\x00th")
	require.Equal(t, "a[31muth", m.Search)
}

func TestFilterCommandClearsSearch(t *testing.T) {
	m, _ := executeCommand(searchModel(), "search auth")
	m, _ = executeCommand(m, "filter")
	require.Equal(t, "", m.Search)
	require.Len(t, rowIDs(m), 4)
}

func TestSearchCommandInTable(t *testing.T) {
	c, ok := lookupCommand("search")
	require.True(t, ok)
	require.Equal(t, ":search [text]", c.helpKey())
	require.NotEmpty(t, c.Desc)
}

func TestBGSavesSearch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	m, _ := executeCommand(searchModel(), "search auth")
	m, _ = executeCommand(m, "bg")
	require.True(t, m.Quitting)

	st, ok := LoadTUIState()
	require.True(t, ok)
	require.Equal(t, "auth", st.Search)

	// The next start restores it once, with the selection.
	fresh := searchModel()
	next, _ := fresh.dispatch(stateRestoreResultMsg{State: persistedState{Selection: "a2", Search: "auth"}, OK: true})
	got := next.(Model)
	require.Equal(t, "auth", got.Search)
	require.Equal(t, "a2", got.SelectedID)
	require.Equal(t, []string{"a1", "a2", "d1"}, rowIDs(got))

	// A state file with no search leaves the model's own alone.
	next, _ = fresh.dispatch(stateRestoreResultMsg{State: persistedState{Selection: "a2"}, OK: true})
	require.Equal(t, "", next.(Model).Search)
}

func TestBGWithoutSearchOmitsKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	m, _ := executeCommand(searchModel(), "bg")
	require.True(t, m.Quitting)
	st, ok := LoadTUIState()
	require.True(t, ok)
	require.Equal(t, "", st.Search)
}

func TestRestoredSearchIsSanitized(t *testing.T) {
	next, _ := searchModel().dispatch(stateRestoreResultMsg{
		State: persistedState{Selection: "a1", Search: "au\x1bth"}, OK: true})
	require.Equal(t, "auth", next.(Model).Search, "the state file is user-editable text")
}
