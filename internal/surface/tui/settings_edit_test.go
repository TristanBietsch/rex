package tui

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

func TestLineEditorKeys(t *testing.T) {
	e := lineEditor{Text: "ab", Max: 5}
	require.True(t, e.Edit(key("c")))
	require.Equal(t, "abc", e.Text)
	require.True(t, e.Edit(key("backspace")))
	require.Equal(t, "ab", e.Text)
	require.True(t, e.Edit(key(" ")))
	require.True(t, e.Edit(key("x")))
	require.Equal(t, "ab x", e.Text)
	require.True(t, e.Edit(key("ctrl+w")))
	require.Equal(t, "ab ", e.Text)
	require.True(t, e.Edit(key("ctrl+u")))
	require.Empty(t, e.Text)
	for range 9 {
		e.Edit(key("z"))
	}
	require.Equal(t, "zzzzz", e.Text, "capped at Max runes")
	require.False(t, e.Edit(key("enter")), "enter and esc belong to the caller")
	require.False(t, e.Edit(key("esc")))
}

func TestLineEditorStripsControlRunesWhenAsked(t *testing.T) {
	e := lineEditor{StripControl: true}
	e.Edit(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a\x1b[31mb\nc")})
	require.Equal(t, "a[31mbc", e.Text)
	raw := lineEditor{}
	raw.Edit(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a\nb")})
	require.Equal(t, "a\nb", raw.Text, "rename keeps what it always took")
}

// enter on a free-form string opens an inline editor; enter again saves it
// through Store.Set, the one validator.
func TestEditFreeString(t *testing.T) {
	dir := t.TempDir()
	m := settingsScreen(t, 120, 50)
	m.Settings.CursorID = "auto_trust_dirs"
	m = settingsPress(m, "enter")
	require.NotNil(t, m.Settings.Edit, "enter opens the editor")
	require.Contains(t, settingsText(m), "enter save")

	m = typeInto(m, dir)
	require.Contains(t, settingsText(m), filepath.Base(dir), "the draft shows in the detail pane")
	require.Equal(t, "", m.Store.String("auto_trust_dirs"), "nothing is stored until enter")
	m = settingsPress(m, "enter")
	require.Nil(t, m.Settings.Edit)
	require.Equal(t, dir, m.Store.String("auto_trust_dirs"))
	require.FileExists(t, m.StorePath, "saved to disk")
	require.Contains(t, settingsText(m), "• ", "now modified")
}

func TestEditFreeStringRejectsRootWithMessage(t *testing.T) {
	m := settingsScreen(t, 120, 50)
	m.Settings.CursorID = "auto_trust_dirs"
	m = typeInto(settingsPress(m, "enter"), "/")
	m = settingsPress(m, "enter")
	require.NotNil(t, m.Settings.Edit, "a refused value keeps the editor open")
	require.NotEmpty(t, m.Settings.Edit.Err)
	require.Contains(t, settingsText(m), m.Settings.Edit.Err, "the message shows in the detail pane")
	require.Equal(t, "", m.Store.String("auto_trust_dirs"))

	m = settingsPress(m, "backspace")
	require.Empty(t, m.Settings.Edit.Err, "editing clears the message")

	m = settingsPress(m, "esc")
	require.Nil(t, m.Settings.Edit)
	require.Equal(t, FocusSettings, m.Focus, "esc cancels the edit, not the page")
	require.Equal(t, "", m.Store.String("auto_trust_dirs"))
}

func TestEditPrefillsCurrentValue(t *testing.T) {
	dir := t.TempDir()
	m := settingsScreen(t, 120, 50)
	require.NoError(t, m.Store.Set("auto_trust_dirs", dir))
	m.Settings.CursorID = "auto_trust_dirs"
	m = settingsPress(m, "enter")
	require.Equal(t, dir, m.Settings.Edit.Text())
	m = settingsPress(m, "ctrl+u", "enter")
	require.Nil(t, m.Settings.Edit)
	require.Equal(t, "", m.Store.String("auto_trust_dirs"), "empty means always ask")
}

func TestPromptGlyphRejectsControlChars(t *testing.T) {
	m := settingsScreen(t, 120, 50)
	m.Settings.CursorID = "prompt_glyph"
	m = settingsPress(m, "enter")
	require.NotNil(t, m.Settings.Edit, "prompt_glyph is free-form too")

	m = settingsPress(m, "ctrl+u")
	m = typeInto(m, "ab")
	m = settingsPress(m, "enter")
	require.NotNil(t, m.Settings.Edit)
	require.Contains(t, m.Settings.Edit.Err, "one character")
	require.Equal(t, "λ", m.Store.String("prompt_glyph"))

	// A pasted escape sequence loses its control runes on the way in.
	m = settingsPress(m, "ctrl+u")
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\x1b")})
	m = next.(Model)
	require.Empty(t, m.Settings.Edit.Text())
	m = settingsPress(m, "enter")
	require.NotEmpty(t, m.Settings.Edit.Err, "an empty glyph is refused")

	m = typeInto(m, "❯")
	m = settingsPress(m, "enter")
	require.Nil(t, m.Settings.Edit)
	require.Equal(t, "❯", m.Store.String("prompt_glyph"))
	require.Equal(t, "❯", promptGlyph(m), "the prompt uses it")
}

// Enum settings still cycle on enter; only free-form strings open the editor.
func TestEnterStillCyclesEnumsAndToggles(t *testing.T) {
	m := settingsScreen(t, 120, 50)
	m.Settings.CursorID = "spinner"
	m = settingsPress(m, "enter")
	require.Nil(t, m.Settings.Edit)
	require.NotEqual(t, "braille", m.Store.String("spinner"))

	m.Settings.CursorID = "reduce_motion"
	m = settingsPress(m, "enter")
	require.Equal(t, "true", m.Store.String("reduce_motion"))

	m.Settings.CursorID = "summary_model" // string with options: cycles, no editor
	m = settingsPress(m, "enter")
	require.Nil(t, m.Settings.Edit)
	m.Settings.CursorID = "lua_config_path" // read-only
	m = settingsPress(m, "enter")
	require.Nil(t, m.Settings.Edit)
}

// While editing, j/k/q and friends type; the wheel does not move the cursor.
func TestEditingTypesKeysAndIgnoresWheel(t *testing.T) {
	m := settingsScreen(t, 120, 50)
	m.Settings.CursorID = "prompt_glyph"
	m = settingsPress(m, "enter", "ctrl+u", "j")
	require.Equal(t, "j", m.Settings.Edit.Text())
	require.Equal(t, "prompt_glyph", m.Settings.CursorID)
	m, _ = updateMouse(m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	require.Equal(t, "prompt_glyph", m.Settings.CursorID)
}
