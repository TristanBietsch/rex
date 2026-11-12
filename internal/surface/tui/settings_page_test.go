package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// pageSettings is the settings page (fullscreen_pages on, the default) on
// the layout board at 120x40 with a throwaway config file.
func pageSettings(t *testing.T) Model {
	t.Helper()
	m := settingsScreen(t, 120, 40)
	require.True(t, pagesOn(m))
	return m
}

// sectionRows is the visible rows of sec, in list order.
func sectionRows(sec settings.Section) []settings.Setting {
	var out []settings.Setting
	for _, s := range settings.Visible() {
		if s.Section == sec {
			out = append(out, s)
		}
	}
	return out
}

func cursorSection(t *testing.T, m Model) settings.Section {
	t.Helper()
	return mustFind(t, m.Settings.CursorID).Section
}

func TestSettingsPagePaneCycle(t *testing.T) {
	m := pageModel(t, 120, 40, FocusSettings)
	require.Equal(t, paneRows, m.Settings.Pane, "a page opens with rows focused")
	m = settingsPress(m, "tab")
	require.Equal(t, paneSections, m.Settings.Pane)
	m = settingsPress(m, "shift+tab")
	require.Equal(t, paneRows, m.Settings.Pane)

	n := pageModel(t, 50, 20, FocusSettings)
	cur := n.Settings.CursorID
	n = settingsPress(n, "tab")
	require.Equal(t, paneRows, n.Settings.Pane, "tab is a no-op on a narrow page")
	require.Equal(t, cur, n.Settings.CursorID, "and does not jump a section")
}

func TestSettingsPageSectionKeys(t *testing.T) {
	m := pageSettings(t)
	secs := settingsSections(m.Settings)
	require.GreaterOrEqual(t, len(secs), 2)
	require.Less(t, len(secs), 9, "the 9 case needs fewer than 9 sections")

	m = settingsPress(m, "2")
	require.Equal(t, sectionRows(secs[1])[0].ID, m.Settings.CursorID, "2: first row of the second section")

	m = settingsPress(m, fmt.Sprint(len(secs)))
	require.Equal(t, secs[len(secs)-1], cursorSection(t, m))
	m = settingsPress(m, "]")
	require.Equal(t, sectionRows(secs[0])[0].ID, m.Settings.CursorID, "] wraps to the first section")
	m = settingsPress(m, "[")
	require.Equal(t, secs[len(secs)-1], cursorSection(t, m), "[ wraps to the last section")

	m = settingsPress(m, "1")
	cur := m.Settings.CursorID
	m = settingsPress(m, "9")
	require.Equal(t, cur, m.Settings.CursorID, "9 past the last section is a no-op")
}

func TestSettingsPageRowsScoped(t *testing.T) {
	audio := sectionRows(settings.SectionAudio)
	m := pageSettings(t)
	m.Settings.CursorID = audio[0].ID
	for _, s := range settingsPageList(m) {
		require.Equal(t, settings.SectionAudio, s.Section, "the page lists the cursor's section only")
	}
	m = settingsPress(m, "k")
	require.Equal(t, audio[0].ID, m.Settings.CursorID, "k on the first row stays")

	m.Settings.CursorID = audio[len(audio)-1].ID
	m = settingsPress(m, "j")
	require.Equal(t, audio[len(audio)-1].ID, m.Settings.CursorID, "j on the last row stays")

	m.Settings.CursorID = audio[0].ID
	m = settingsPress(m, "j")
	require.Equal(t, audio[1].ID, m.Settings.CursorID, "j moves within the section")
}

func TestSettingsPageLeftPane(t *testing.T) {
	m := pageSettings(t)
	secs := settingsSections(m.Settings)
	before := m.Store.Snapshot()

	m = settingsPress(m, "tab", "j")
	require.Equal(t, paneSections, m.Settings.Pane)
	require.Equal(t, sectionRows(secs[1])[0].ID, m.Settings.CursorID, "j on the left pane: next section")
	m = settingsPress(m, "k", "k")
	require.Equal(t, sectionRows(secs[0])[0].ID, m.Settings.CursorID, "k clamps at the first section")
	m = settingsPress(m, "j")

	m.Settings.CursorID = "spinner"
	m = settingsPress(m, "h", "r", "-", "+", "?", " ")
	require.Equal(t, before, m.Store.Snapshot(), "value keys on the left pane change nothing")
	require.Equal(t, paneSections, m.Settings.Pane)

	m = settingsPress(m, "enter")
	require.Equal(t, paneRows, m.Settings.Pane, "enter focuses the rows")
	require.Equal(t, before, m.Store.Snapshot(), "enter on the left pane toggles nothing")
	m = settingsPress(m, "tab", "l")
	require.Equal(t, paneRows, m.Settings.Pane, "l focuses the rows")
}

func TestSettingsPageHOnFixedRowStays(t *testing.T) {
	m := pageSettings(t)
	m.Settings.CursorID = "mouse_enabled" // bool: not adjustable
	before := m.Store.Snapshot()
	m = settingsPress(m, "h")
	require.Equal(t, paneRows, m.Settings.Pane, "h does not move focus")
	require.Equal(t, before, m.Store.Snapshot(), "h on a non-adjustable row changes nothing")
}

func TestSettingsPageSearchEscRestores(t *testing.T) {
	m := pageSettings(t)
	m.Settings.CursorID = "spinner"
	m = typeInto(settingsPress(m, "/"), "volume")
	require.Equal(t, "master_volume", m.Settings.CursorID)
	m = settingsPress(m, "esc")
	require.Equal(t, "spinner", m.Settings.CursorID, "esc while typing restores the cursor")
	require.Empty(t, m.Settings.Search)
	require.Equal(t, FocusSettings, m.Focus)

	m = typeInto(settingsPress(m, "/"), "volume")
	m = settingsPress(m, "enter")
	require.Equal(t, "master_volume", m.Settings.CursorID, "enter keeps the match")
	m = settingsPress(m, "esc")
	require.Equal(t, "master_volume", m.Settings.CursorID, "clearing a kept search keeps the cursor")
	require.Empty(t, m.Settings.Search)
	require.Equal(t, FocusSettings, m.Focus)
}

func TestSettingsPageSearchSpansSections(t *testing.T) {
	m := pageSettings(t)
	m.Settings.Pane = paneSections
	m = typeInto(settingsPress(m, "/"), "on")
	require.Equal(t, paneRows, m.Settings.Pane, "/ focuses the rows")
	secs := map[settings.Section]bool{}
	for _, s := range settingsPageList(m) {
		secs[s.Section] = true
	}
	require.GreaterOrEqual(t, len(secs), 2, "a search lists rows from every section")
}

func TestSettingsPageEditAndReset(t *testing.T) {
	m := pageSettings(t)
	m.Settings.CursorID = "prompt_glyph"
	m = settingsPress(m, "enter")
	require.NotNil(t, m.Settings.Edit, "enter edits a free-form row")
	m = settingsPress(m, "esc")
	require.Nil(t, m.Settings.Edit)
	require.Equal(t, FocusSettings, m.Focus)

	m.Settings.CursorID = "spinner"
	def := m.Store.String("spinner")
	m = settingsPress(m, "l")
	require.NotEqual(t, def, m.Store.String("spinner"), "l changes the value")
	m = settingsPress(m, "r")
	require.Equal(t, def, m.Store.String("spinner"), "r restores the default")
}

func TestSettingsPageFlagFlipLive(t *testing.T) {
	m := pageModel(t, 120, 40, FocusSettings)
	require.Contains(t, settingsText(m), "∴ REX › SETTINGS")
	m.Settings.CursorID = "fullscreen_pages"
	m = settingsPress(m, "enter")
	require.False(t, pagesOn(m), "enter on the row turns pages off")
	require.Equal(t, "fullscreen_pages", m.Settings.CursorID, "the cursor stays")

	text := settingsText(m)
	require.NotContains(t, text, "∴ REX › SETTINGS", "the overlay draws now")
	require.Contains(t, text, layoutSessions(goldenNow)[0].Slug, "the board shows behind the overlay")

	m = settingsPress(m, "tab")
	require.Equal(t, sectionRows(settings.SectionSpawn)[0].ID, m.Settings.CursorID, "tab jumps a section again")

	require.NoError(t, m.Store.Set("fullscreen_pages", true))
	require.Contains(t, settingsText(m), "∴ REX › SETTINGS", "the page again")
}

func TestSettingsOverlayKeysUnchanged(t *testing.T) {
	m := overlayMode(t, settingsScreen(t, 120, 40))
	m.Settings.CursorID = "spinner"
	before := *m.Settings
	store := m.Store.Snapshot()
	m = settingsPress(m, "1", "[", "]", "q")
	require.Equal(t, FocusSettings, m.Focus, "q does not close the overlay")
	require.Equal(t, before.CursorID, m.Settings.CursorID)
	require.Equal(t, before.Pane, m.Settings.Pane)
	require.Equal(t, store, m.Store.Snapshot())

	m = settingsPress(m, "tab")
	require.Equal(t, sectionRows(settings.SectionBehavior)[0].ID, m.Settings.CursorID, "tab jumps a section")
	require.Equal(t, paneRows, m.Settings.Pane, "tab does not switch panes")
}

func TestSettingsPageWheel(t *testing.T) {
	m := pageSettings(t)
	require.NoError(t, m.Store.Set("mouse_enabled", true))
	secs := settingsSections(m.Settings)
	m.Settings.Pane = paneSections
	m, _ = updateMouse(m, wheel(true))
	require.Equal(t, sectionRows(secs[1])[0].ID, m.Settings.CursorID, "wheel on the sections: next section")

	m.Settings.Pane = paneRows
	rows := sectionRows(secs[1])
	m, _ = updateMouse(m, wheel(true))
	require.Equal(t, rows[1].ID, m.Settings.CursorID, "wheel on the rows: next row")
	m.Settings.CursorID = rows[len(rows)-1].ID
	m, _ = updateMouse(m, wheel(true))
	require.Equal(t, rows[len(rows)-1].ID, m.Settings.CursorID, "the wheel stays in the section")
}

func TestSettingsModifiedIn(t *testing.T) {
	m := pageSettings(t)
	require.Zero(t, settingsModifiedIn(m, settings.SectionAppearance))
	require.NoError(t, m.Store.Set("spinner", "moon"))
	require.Equal(t, 1, settingsModifiedIn(m, settings.SectionAppearance))
	require.Zero(t, settingsModifiedIn(m, settings.SectionAudio))
}

func TestSettingsSectionOfUnknownCursor(t *testing.T) {
	m := pageSettings(t)
	m.Settings.CursorID = "no_such_row"
	require.Equal(t, settingsSections(m.Settings)[0], settingsSectionOf(m.Settings))
	require.Equal(t, sectionRows(settingsSections(m.Settings)[0])[0].ID, settingsGoSection(m.Settings, -5))
	secs := settingsSections(m.Settings)
	require.Equal(t, sectionRows(secs[len(secs)-1])[0].ID, settingsGoSection(m.Settings, 99))
	require.False(t, strings.Contains(fmt.Sprint(secs), string(settings.SectionOnboarding)), "no onboarding rows in this model")
}
