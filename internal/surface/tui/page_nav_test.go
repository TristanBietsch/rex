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
)

// Key-sequence tests of the full-screen pages' vim-style navigation (the
// shared navigator in nav.go): settings, help and the new-agent page.

// navKeys names the special keys the tests press.
var navKeys = map[string]tea.KeyType{
	"enter": tea.KeyEnter, "esc": tea.KeyEsc, "tab": tea.KeyTab, "shift+tab": tea.KeyShiftTab,
	"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
	"home": tea.KeyHome, "end": tea.KeyEnd, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
	"ctrl+d": tea.KeyCtrlD, "ctrl+u": tea.KeyCtrlU, "ctrl+f": tea.KeyCtrlF, "ctrl+b": tea.KeyCtrlB,
	"ctrl+s": tea.KeyCtrlS, "ctrl+g": tea.KeyCtrlG, "ctrl+e": tea.KeyCtrlE, "ctrl+t": tea.KeyCtrlT,
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

// ── New-agent page: Compose ─────────────────────────────────────────────────

// naWithItems is the page with a typed task and three pasted context items.
func naWithItems(t *testing.T) Model {
	t.Helper()
	m := naOpen(t, naModel(t))
	m = naType(m, "fix it")
	for i := range 3 {
		require.NoError(t, addPaste(m.NewAgent, fmt.Sprintf("paste body %d\n", i)+strings.Repeat("line\n", naChipLines+1)))
	}
	return m
}

func TestPageNav_NAEscLeavesTheEditorFirst(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naType(m, "ab")
	m = navPress(m, "esc")
	require.Equal(t, FocusNewAgent, m.Focus, "esc in the editor leaves the editor, not the page")
	m = navPress(m, "j", "k", "x")
	require.Equal(t, "ab", m.NewAgent.Task.String(), "j, k and x do not type outside the editor")
	m = navPress(m, "esc")
	require.Equal(t, FocusBoard, m.Focus, "a second esc closes the page")
	require.NotNil(t, m.newAgentDraft)
}

func TestPageNav_NAIAndEnterEdit(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = navPress(m, "esc", "i", "q")
	require.Equal(t, "q", m.NewAgent.Task.String(), "i types again")
	m = navPress(m, "esc", "enter", "w")
	require.Equal(t, "qw", m.NewAgent.Task.String(), "enter on the task edits it")
	require.False(t, m.NewAgent.Launching, "enter outside the editor never launches from the task")
}

func TestPageNav_NAJKCrossSections(t *testing.T) {
	m := naWithItems(t)
	m = navPress(m, "esc")
	require.Equal(t, naTask, m.NewAgent.Section)
	m = navPress(m, "k")
	require.Equal(t, naAgent, m.NewAgent.Section, "k from the task: the agent row")
	m = navPress(m, "k")
	require.Equal(t, naAgent, m.NewAgent.Section, "k clamps at the top")
	m = navPress(m, "j", "j")
	require.Equal(t, naContext, m.NewAgent.Section, "j from the task: the context list")
	require.Zero(t, m.NewAgent.ItemIdx)
	m = navPress(m, "j", "j")
	require.Equal(t, 2, m.NewAgent.ItemIdx, "j walks the items")
	m = navPress(m, "j")
	require.Equal(t, naTrust, m.NewAgent.Section, "j past the last item: trust")
	m = navPress(m, "k")
	require.Equal(t, naContext, m.NewAgent.Section)
	require.Equal(t, 2, m.NewAgent.ItemIdx, "k comes back onto the last item")
	m = navPress(m, "G")
	require.Equal(t, naLaunchRow, m.NewAgent.Section, "G: the launch row")
	m = navPress(m, "g", "g")
	require.Equal(t, naAgent, m.NewAgent.Section, "gg: the agent row")
}

func TestPageNav_NARailFocus(t *testing.T) {
	m := naWithItems(t)
	m = navPress(m, "esc", "j")
	require.Equal(t, naContext, m.NewAgent.Section)
	m = navPress(m, "h")
	require.True(t, m.NewAgent.OnRail, "h focuses the step rail")
	m = navPress(m, "j")
	require.Equal(t, naTrust, m.NewAgent.Section, "j on the rail: the next step, not the next item")
	m = navPress(m, "k", "l")
	require.False(t, m.NewAgent.OnRail, "l goes back to the content")
	require.Equal(t, naContext, m.NewAgent.Section)
	m = navPress(m, "h", "enter")
	require.False(t, m.NewAgent.OnRail, "enter on the rail opens the step")
}

func TestPageNav_NAContextKeys(t *testing.T) {
	m := naWithItems(t)
	m = navPress(m, "esc", "j")
	require.Equal(t, naContext, m.NewAgent.Section)
	m = navPress(m, "d")
	require.Len(t, m.NewAgent.Items, 2, "d removes the item")
	m = navPress(m, "x")
	require.Len(t, m.NewAgent.Items, 1, "x too")
	_, cmd := updateKey(m, vk("e"))
	require.NotNil(t, cmd, "e edits a paste in $EDITOR")
	m = navPress(m, "enter")
	require.Equal(t, naPreview, m.NewAgent.Screen)
	m = navPress(m, "esc")
	require.Equal(t, naCompose, m.NewAgent.Screen)
}

func TestPageNav_NAEnterOnLaunchRow(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naType(m, "ship it")
	m = navPress(m, "esc", "G")
	require.Equal(t, naLaunchRow, m.NewAgent.Section)
	require.Contains(t, naView(m), "launch")
	m, cmd := updateKey(m, vk("enter"))
	require.NotNil(t, cmd)
	require.True(t, m.NewAgent.Launching, "enter on the launch row launches")
}

func TestPageNav_NATrustCycles(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = navPress(m, "esc", "4")
	require.Equal(t, naTrust, m.NewAgent.Section)
	require.Equal(t, trustAsk, m.NewAgent.Trust)
	m = navPress(m, "enter")
	require.Equal(t, trustSession, m.NewAgent.Trust, "enter on trust: this session")
	m = navPress(m, " ")
	require.Equal(t, trustAlways, m.NewAgent.Trust, "space: always")
	m = navPress(m, "enter")
	require.Equal(t, trustAsk, m.NewAgent.Trust, "and back to ask")
}

func TestPageNav_NAWheel(t *testing.T) {
	m := naOpen(t, naModel(t))
	require.NoError(t, m.Store.Set("mouse_enabled", true))
	m, _ = updateMouse(m, wheelAt(true, 60, 10))
	require.Equal(t, naContext, m.NewAgent.Section, "the wheel moves as j")
}

func TestPageNav_NANarrowHL(t *testing.T) {
	m := naModel(t)
	m.Width, m.Height = 50, 20
	m = naOpen(t, m)
	m = navPress(m, "esc", "l")
	require.Equal(t, naContext, m.NewAgent.Section, "narrow: l is the next step")
	m = navPress(m, "h", "h")
	require.Equal(t, naAgent, m.NewAgent.Section, "h the previous one")
}

// ── New-agent page: Agent screen ────────────────────────────────────────────

func TestPageNav_NAAgentHLColumns(t *testing.T) {
	m := naAgentOpen(t, naModel(t))
	m = naPickTool(t, m, "claude")
	effort := m.NewAgent.choice().Effort
	m = navPress(m, "l")
	require.Equal(t, naPaneModels, m.NewAgent.Pane, "l: the models")
	m = navPress(m, "h")
	require.Equal(t, naPaneAgents, m.NewAgent.Pane, "h: the agents")
	require.Equal(t, effort, m.NewAgent.choice().Effort, "h/l never change the effort")
}

func TestPageNav_NAAgentJIntoEffort(t *testing.T) {
	m := naAgentOpen(t, naModel(t))
	m = naPickTool(t, m, "claude")
	m = navPress(m, "l", "G")
	models := m.NewAgent.filteredIdx()
	require.Equal(t, models[len(models)-1], m.NewAgent.modelIdx(), "G: the last model")
	m = navPress(m, "j")
	require.Equal(t, naPaneEffort, m.NewAgent.Pane, "j past the last model: effort")
	m = pressN(m, "left", 6)
	require.Zero(t, m.NewAgent.EffortIdx, "← on effort steps it, down to the first")
	require.Equal(t, naPaneEffort, m.NewAgent.Pane, "← stays on the effort row")
	m = navPress(m, "right")
	require.Equal(t, 1, m.NewAgent.EffortIdx, "→ steps it up")
	m = navPress(m, "k")
	require.Equal(t, naPaneModels, m.NewAgent.Pane, "k comes back to the models")
	m = navPress(m, "g", "g")
	require.Equal(t, models[0], m.NewAgent.modelIdx(), "gg: the first model")
}

func TestPageNav_NAAgentManyModels(t *testing.T) {
	m := naModel(t)
	m.DaemonTools = cursorModels(t)
	m = naPickTool(t, naAgentOpen(t, m), "cursor")
	m = navPress(m, "l", "ctrl+d")
	require.Greater(t, m.NewAgent.modelIdx(), 2, "ctrl+d moves half a window of models")
	m = navPress(m, "G")
	require.Equal(t, 199, m.NewAgent.modelIdx())
	require.Contains(t, naView(m), "model-199")
	m = navPress(m, "/", "s", "o", "n", "enter", "j")
	require.Equal(t, "sonnet-4.6-thinking", m.NewAgent.choice().Model, "j after enter moves through the matches")
}
