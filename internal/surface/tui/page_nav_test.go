package tui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

// Key-sequence tests of the full-screen pages' vim-style navigation (the
// shared navigator in nav.go): settings and help (the new-agent page is in newagent_nav_test.go).

// navKeys names the special keys the tests press.
var navKeys = map[string]tea.KeyType{
	"enter": tea.KeyEnter, "esc": tea.KeyEsc, "tab": tea.KeyTab, "shift+tab": tea.KeyShiftTab,
	"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
	"home": tea.KeyHome, "end": tea.KeyEnd, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
	"ctrl+d": tea.KeyCtrlD, "ctrl+u": tea.KeyCtrlU, "ctrl+f": tea.KeyCtrlF, "ctrl+b": tea.KeyCtrlB,
	"ctrl+s": tea.KeyCtrlS, "ctrl+g": tea.KeyCtrlG, "ctrl+e": tea.KeyCtrlE, "ctrl+t": tea.KeyCtrlT,
	"ctrl+o": tea.KeyCtrlO, "ctrl+a": tea.KeyCtrlA, "ctrl+j": tea.KeyCtrlJ,
	"backspace": tea.KeyBackspace, "delete": tea.KeyDelete, " ": tea.KeySpace,
}

// vk is the key message for name: a special key above, else typed runes.
func vk(name string) tea.KeyMsg {
	if t, ok := navKeys[name]; ok {
		return tea.KeyMsg{Type: t}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

// navPress sends each key through Update.
func navPress(m Model, keys ...string) Model {
	for _, k := range keys {
		next, _ := m.Update(vk(k))
		m = next.(Model)
	}
	return m
}

// pressN presses k n times.
func pressN(m Model, k string, n int) Model {
	for range n {
		m = navPress(m, k)
	}
	return m
}

// wheelAt is a wheel event at screen column x, row y.
func wheelAt(down bool, x, y int) tea.MouseMsg {
	msg := wheel(down)
	msg.X, msg.Y = x, y
	return msg
}

// ── Navigator ───────────────────────────────────────────────────────────────

func TestNav_ScrollKeepsContextAndMovesOneRow(t *testing.T) {
	const n, h = 40, 10
	// The screen line of the first row shown moves one line at a time: the
	// ↑ marker takes the first line when the window first scrolls.
	top, prevLine := 0, 0
	for cur := range n {
		top = navScroll(top, cur, n, h)
		start, end, up, down := navSpan(top, n, h)
		line := start - b2i(up)
		require.LessOrEqual(t, line-prevLine, 1, "cursor %d: the window moves one row at a time", cur)
		require.True(t, cur >= start && cur < end, "cursor %d is shown (%d..%d)", cur, start, end)
		if down {
			require.LessOrEqual(t, cur, end-1-navScrollOff, "cursor %d keeps %d rows below it", cur, navScrollOff)
		}
		if up {
			require.GreaterOrEqual(t, cur, start+navScrollOff, "cursor %d keeps %d rows above it", cur, navScrollOff)
		}
		if cur < h-1-navScrollOff-1 {
			require.Zero(t, top, "cursor %d: no scroll before the margin is reached", cur)
		}
		prevLine = line
	}
	require.True(t, navFullAt(top, n, h), "at the last row the window ends on the last row")
	for cur := n - 1; cur >= 0; cur-- {
		next := navScroll(top, cur, n, h)
		s0, _, u0, _ := navSpan(top, n, h)
		s1, _, u1, _ := navSpan(next, n, h)
		require.LessOrEqual(t, (s0-b2i(u0))-(s1-b2i(u1)), 1, "cursor %d going up: one row at a time", cur)
		top = next
	}
	require.Zero(t, top)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func navFullAt(top, n, h int) bool {
	_, end, _, _ := navSpan(top, n, h)
	return end == n
}

func TestNav_WindowMarkersHideNoRow(t *testing.T) {
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = fmt.Sprintf("row %02d", i)
	}
	var v listView
	shown := map[string]bool{}
	for cur := range lines {
		out := v.window("k", lines, cur, 8, "")
		require.Len(t, out, 8)
		require.Contains(t, out, lines[cur])
		for _, l := range out {
			shown[l] = true
		}
	}
	for _, l := range lines {
		require.True(t, shown[l], "%s was shown at some point while j walked the list", l)
	}
}

func TestNav_VimMotion(t *testing.T) {
	pending := false
	require.Equal(t, navPending, vimMotion("g", &pending))
	require.Equal(t, navFirst, vimMotion("g", &pending), "gg")
	require.Equal(t, navPending, vimMotion("g", &pending))
	require.Equal(t, navDown, vimMotion("j", &pending), "g then j: j")
	require.False(t, pending)
	require.Equal(t, navLast, vimMotion("G", &pending))
	require.Equal(t, 5, navTarget(navHalfDown, 0, 100, 10))
	require.Equal(t, 9, navTarget(navPageDown, 0, 100, 10))
	require.Equal(t, 0, navTarget(navUp, 0, 100, 10), "k clamps at the top")
	require.Equal(t, 99, navTarget(navDown, 99, 100, 10), "j clamps at the bottom")
}

// ── Settings page ───────────────────────────────────────────────────────────

func TestPageNav_SettingsJKCrossSections(t *testing.T) {
	m := pageSettings(t)
	secs := settingsSections(m.Settings)
	first, second := sectionRows(secs[0]), sectionRows(secs[1])
	m.Settings.CursorID = first[len(first)-1].ID
	m = navPress(m, "j")
	require.Equal(t, second[0].ID, m.Settings.CursorID, "j on a section's last row goes on to the next section")
	require.Contains(t, pageView(m)[3], string(secs[1]), "the rows pane shows the new section")
	m = navPress(m, "k")
	require.Equal(t, first[len(first)-1].ID, m.Settings.CursorID, "k comes back")
	m = navPress(m, "down", "up")
	require.Equal(t, first[len(first)-1].ID, m.Settings.CursorID, "the arrows move as j/k")
}

func TestPageNav_SettingsHLMovePanes(t *testing.T) {
	m := pageSettings(t)
	before := m.Store.Snapshot()
	m.Settings.CursorID = "theme" // an enum: ←/→ change it
	m = navPress(m, "l")
	require.Equal(t, paneRows, m.Settings.Pane)
	require.Equal(t, before, m.Store.Snapshot(), "l does not change a value")
	m = navPress(m, "h")
	require.Equal(t, paneSections, m.Settings.Pane, "h focuses the sections")
	require.Equal(t, before, m.Store.Snapshot(), "h does not change a value")
	m = navPress(m, "l")
	require.Equal(t, paneRows, m.Settings.Pane, "l focuses the rows")

	m = navPress(m, "right")
	require.NotEqual(t, before, m.Store.Snapshot(), "→ still steps an enum (the value contract)")
	m = navPress(m, "left")
	require.Equal(t, before, m.Store.Snapshot(), "← steps it back")
	require.Equal(t, paneRows, m.Settings.Pane)

	m.Settings.CursorID = settingsNoValueRow(t, m)
	m = navPress(m, "left")
	require.Equal(t, paneSections, m.Settings.Pane, "← on a row without a value focuses the sections")
}

// settingsNoValueRow is a row ← and → cannot change (read-only or an
// onboarding row).
func settingsNoValueRow(t *testing.T, m Model) string {
	t.Helper()
	for _, s := range settingsList(m.Settings) {
		if !settingsHasValue(m, s) {
			return s.ID
		}
	}
	t.Skip("every row has a value")
	return ""
}

func TestPageNav_SettingsRemembersRowPerSection(t *testing.T) {
	m := pageSettings(t)
	secs := settingsSections(m.Settings)
	rows := sectionRows(secs[0])
	require.GreaterOrEqual(t, len(rows), 3)
	m = navPress(m, "1", "j", "j")
	require.Equal(t, rows[2].ID, m.Settings.CursorID)
	m = navPress(m, "]", "[")
	require.Equal(t, rows[2].ID, m.Settings.CursorID, "] then [: back on the row left")
	m = navPress(m, "h", "j", "k")
	require.Equal(t, rows[2].ID, m.Settings.CursorID, "the sections pane comes back to the row left too")
	m = navPress(m, "l")
	require.Equal(t, rows[2].ID, m.Settings.CursorID, "l keeps the row")
}

func TestPageNav_SettingsTopBottomAndPages(t *testing.T) {
	m := pageSettings(t)
	all := settingsNavList(m)
	m = navPress(m, "G")
	require.Equal(t, all[len(all)-1].ID, m.Settings.CursorID, "G: the last row")
	m = navPress(m, "g", "g")
	require.Equal(t, all[0].ID, m.Settings.CursorID, "gg: the first row")
	m = navPress(m, "ctrl+d")
	i := slices.IndexFunc(all, func(s settings.Setting) bool { return s.ID == m.Settings.CursorID })
	require.Greater(t, i, 1, "ctrl+d moves half a window")
	m = navPress(m, "ctrl+u")
	require.Equal(t, all[0].ID, m.Settings.CursorID, "ctrl+u comes back")
	m = navPress(m, "ctrl+f")
	j := slices.IndexFunc(all, func(s settings.Setting) bool { return s.ID == m.Settings.CursorID })
	require.Greater(t, j, i, "ctrl+f moves a whole window")
}

func TestPageNav_SettingsBracketsClamp(t *testing.T) {
	m := pageSettings(t)
	secs := settingsSections(m.Settings)
	m = navPress(m, "1", "[")
	require.Equal(t, secs[0], cursorSection(t, m), "[ on the first section stays (no wrap)")
	m = navPress(m, fmt.Sprint(len(secs)), "]")
	require.Equal(t, secs[len(secs)-1], cursorSection(t, m), "] on the last section stays")
}

// settingsLongSection is a section with more rows than the rows pane shows
// at w x h, and that height.
func settingsLongSection(t *testing.T, w, h int) (settings.Section, int) {
	t.Helper()
	m := settingsScreen(t, w, h)
	for _, sec := range settingsSections(m.Settings) {
		rows := sectionRows(sec)
		m.Settings.CursorID = rows[0].ID
		_ = m.View()
		if vh := m.Settings.rowsView.H; vh > 0 && len(rows) > vh {
			return sec, vh
		}
	}
	t.Skipf("no section scrolls at %dx%d", w, h)
	return "", 0
}

func TestPageNav_SettingsScrollOff(t *testing.T) {
	sec, vh := settingsLongSection(t, 80, 24)
	m := settingsScreen(t, 80, 24)
	rows := sectionRows(sec)
	m.Settings.CursorID = rows[0].ID
	steps := vh - navScrollOff - 2 // the last row before the margin
	m = pressN(m, "j", steps)
	require.Equal(t, rows[steps].ID, m.Settings.CursorID)
	require.NotContains(t, settingsText(m), "↑ more", "no scroll while the cursor is above the margin")
	m = navPress(m, "j")
	v := settingsText(m)
	require.Contains(t, v, "↑ more", "the next j scrolls")
	require.Contains(t, v, rows[steps+1+navScrollOff].Label, "keeping %d rows under the cursor", navScrollOff)
}

func TestPageNav_SettingsWheelUnderPointer(t *testing.T) {
	m := pageSettings(t)
	require.NoError(t, m.Store.Set("mouse_enabled", true))
	secs := settingsSections(m.Settings)
	m = navPress(m, "1")
	require.Equal(t, paneRows, m.Settings.Pane)
	m, _ = updateMouse(m, wheelAt(true, 4, 8))
	require.Equal(t, secs[1], cursorSection(t, m), "the wheel over the sections moves the section")
	m, _ = updateMouse(m, wheelAt(true, 60, 8))
	require.Equal(t, sectionRows(secs[1])[1].ID, m.Settings.CursorID, "over the rows: one row, as j")
}

// ── Help page ───────────────────────────────────────────────────────────────

func TestPageNav_HelpJScrollsOneRowNoneHidden(t *testing.T) {
	g := longGroup(t, 80, 24)
	m := helpPagePress(pageHelp(t, 80, 24), fmt.Sprint(g+1))
	rows := renderGroup(helpGroups(m)[g], 50)
	second := strings.TrimSpace(ansiStrip(rows[1]))
	m = navPress(m, "j")
	require.Equal(t, 1, m.Help.Scroll)
	require.Contains(t, rightPane(m), second[:min(len(second), 12)], "j hides one row; the ↑ marker hides none")
}

func ansiStrip(s string) string { return strings.TrimSpace(ansi.Strip(s)) }

func TestPageNav_HelpHalfPageAndGg(t *testing.T) {
	g := longGroup(t, 80, 24)
	m := helpPagePress(pageHelp(t, 80, 24), fmt.Sprint(g+1))
	lay := helpLayoutFor(m, 80, 24)
	m = navPress(m, "ctrl+d")
	require.Equal(t, min(max((lay.page+1)/2, 1), lay.maxScroll), m.Help.Scroll, "ctrl+d scrolls half a window")
	m = navPress(m, "ctrl+u")
	require.Zero(t, m.Help.Scroll)
	m = navPress(m, "ctrl+f")
	require.Equal(t, min(lay.page, lay.maxScroll), m.Help.Scroll, "ctrl+f scrolls a window")
	m = navPress(m, "g")
	require.NotZero(t, m.Help.Scroll, "one g waits for the second")
	m = navPress(m, "g")
	require.Zero(t, m.Help.Scroll, "gg: the top")
	require.Zero(t, m.Help.Group, "gg: the first group")
}

func TestPageNav_HelpJCrossesGroups(t *testing.T) {
	m := pageHelp(t, 120, 40)
	m = navPress(m, "1")
	lay := helpLayoutFor(m, 120, 40)
	m = pressN(m, "j", lay.maxScroll+1)
	require.Equal(t, 1, m.Help.Group, "j past a group's end goes on to the next group")
	require.Zero(t, m.Help.Scroll)
	m = navPress(m, "k")
	require.Zero(t, m.Help.Group, "k at a group's top goes back")
	require.Equal(t, lay.maxScroll, m.Help.Scroll, "to its end")
}

func TestPageNav_HelpHLAndBrackets(t *testing.T) {
	m := pageHelp(t, 120, 40)
	m = navPress(m, "h")
	require.Equal(t, paneSections, m.Help.Pane, "h focuses the groups")
	m = navPress(m, "l")
	require.Equal(t, paneRows, m.Help.Pane, "l focuses the rows")
	m = navPress(m, "1", "[")
	require.Zero(t, m.Help.Group, "[ on the first group stays (no wrap)")
	m = navPress(m, "9", "]")
	require.Equal(t, 8, m.Help.Group, "] on the last group stays")
}

func TestPageNav_HelpWheelIsOneRow(t *testing.T) {
	g := longGroup(t, 80, 24)
	m := helpPagePress(pageHelp(t, 80, 24), fmt.Sprint(g+1))
	m.Store = settings.NewStore()
	require.NoError(t, m.Store.Set("mouse_enabled", true))
	m, _ = updateMouse(m, wheelAt(true, 60, 8))
	require.Equal(t, 1, m.Help.Scroll, "the wheel scrolls one row, as j")
}

// The accent row (feat/accent-color) keeps its keys under the navigator:
// ←/→ cycle the names, e and # open the inline editor, h/l only move focus.
func TestPageNav_SettingsAccentRowKeys(t *testing.T) {
	t.Cleanup(func() { applyTheme("default") })
	m := accentPage(t)
	m = navPress(m, "right")
	require.Equal(t, theme.AccentIDs()[0], m.Store.Get("accent_color"), "→ cycles to the first name")
	m = navPress(m, "left")
	require.Equal(t, "theme", m.Store.Get("accent_color"), "← back")
	m = navPress(m, "l")
	require.Equal(t, "theme", m.Store.Get("accent_color"), "l never changes it")
	m = navPress(m, "e")
	require.NotNil(t, m.Settings.Edit, "e opens the editor")
	m = navPress(m, "j", "k", "h")
	require.Equal(t, "jkh", m.Settings.Edit.Text(), "in the editor keys type, the navigator stays out")
	m = navPress(m, "esc")
	require.Nil(t, m.Settings.Edit)
	m = navPress(m, "#", "1", "2", "3", "enter")
	require.Nil(t, m.Settings.Edit)
	require.Equal(t, "#123", m.Store.Get("accent_color"), "# types a color and enter saves it")
	m = navPress(m, "h")
	require.Equal(t, paneSections, m.Settings.Pane, "h focuses the sections")
	require.Equal(t, "accent_color", m.Settings.CursorID)
	m = navPress(m, "l")
	require.Equal(t, "accent_color", m.Settings.CursorID, "and l comes back to the row")
}

func TestNav_ShortWindowShowsTheCursor(t *testing.T) {
	lines := make([]string, 12)
	for i := range lines {
		lines[i] = fmt.Sprintf("row %02d", i)
	}
	for _, h := range []int{1, 2, 3} {
		var v listView
		for cur := range lines {
			out := v.window("k", lines, cur, h, "")
			require.LessOrEqual(t, len(out), h, "h %d cursor %d", h, cur)
			require.Contains(t, out, lines[cur], "h %d cursor %d", h, cur)
		}
	}
}

// Review finding: a g then another key is not gg.
func TestPageNav_GThenOtherKeyIsNotGg(t *testing.T) {
	m := pageSettings(t)
	all := settingsNavList(m)
	m = navPress(m, "G", "g", "h", "l", "g")
	require.Equal(t, all[len(all)-1].ID, m.Settings.CursorID, "g h l g is no gg")

	h := pageHelp(t, 120, 40)
	h = navPress(h, "3", "g", "]", "g")
	require.Equal(t, 3, h.Help.Group, "g ] g is no gg")

	n := naAt(naOpen(t, naModel(t)), naStepAgent)
	n = navPress(n, "j", "g", "2", "g")
	require.Equal(t, naStepModel, n.NewAgent.Step, "g 2 g is no gg")
}
