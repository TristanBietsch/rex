package tui

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// cmdMsgs runs cmd (flattening batches) and returns the messages it yields.
// Only use it on commands that return immediately (no ticks / listeners).
func cmdMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, cmdMsgs(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func hasMsgLike(msgs []tea.Msg, want tea.Msg) bool {
	for _, m := range msgs {
		if reflect.TypeOf(m) == reflect.TypeOf(want) {
			return true
		}
	}
	return false
}

var (
	enableMouseMsg  = tea.EnableMouseCellMotion()
	disableMouseMsg = tea.DisableMouse()
)

func mouseStore(t *testing.T, on bool) *settings.Store {
	t.Helper()
	s := settings.NewStore()
	require.NoError(t, s.Set("mouse_enabled", on))
	return s
}

func click(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
}

func wheel(down bool) tea.MouseMsg {
	b := tea.MouseButtonWheelUp
	if down {
		b = tea.MouseButtonWheelDown
	}
	return tea.MouseMsg{X: 10, Y: 10, Button: b, Action: tea.MouseActionPress}
}

// Regression: the board never turned mouse reporting on, so click-to-select
// could not work. The handoff from the splash must enable it.
func TestHandOffToBoard_EnablesMouse(t *testing.T) {
	m := Model{Focus: FocusBoot, Store: settings.NewStore()}
	m, cmd := m.handOffToBoard()
	require.True(t, m.mouseOn)
	require.True(t, hasMsgLike(cmdMsgs(cmd), enableMouseMsg))
}

func TestHandOffToBoard_MouseDisabledSetting(t *testing.T) {
	m := Model{Focus: FocusBoot, Store: mouseStore(t, false)}
	m, cmd := m.handOffToBoard()
	require.False(t, m.mouseOn)
	require.False(t, hasMsgLike(cmdMsgs(cmd), enableMouseMsg))
}

func TestSyncMouse_OnlyEmitsOnTransition(t *testing.T) {
	m := Model{Focus: FocusBoard, Store: settings.NewStore()}
	m, cmd := syncMouse(m, "test")
	require.Equal(t, []tea.Msg{enableMouseMsg}, cmdMsgs(cmd))
	m, cmd = syncMouse(m, "test")
	require.Nil(t, cmd, "already on")

	require.NoError(t, m.Store.Set("mouse_enabled", false))
	m, cmd = syncMouse(m, "test")
	require.Equal(t, []tea.Msg{disableMouseMsg}, cmdMsgs(cmd))
	require.False(t, m.mouseOn)

	// The splash never captures.
	m = Model{Focus: FocusBoot}
	_, cmd = syncMouse(m, "test")
	require.Nil(t, cmd)
}

// Toggling mouse_enabled in the settings page applies immediately.
func TestSettings_MouseEnabledAppliesLive(t *testing.T) {
	m := Model{
		Focus:     FocusSettings,
		Store:     settings.NewStore(),
		StorePath: filepath.Join(t.TempDir(), "config.yaml"),
		Settings:  &SettingsState{CursorID: "mouse_enabled"},
		mouseOn:   true,
	}
	m, cmd := updateSettingsKey(m, key("enter"))
	require.Equal(t, false, m.Store.Get("mouse_enabled"))
	require.False(t, m.mouseOn)
	require.Equal(t, []tea.Msg{disableMouseMsg}, cmdMsgs(cmd))

	m, cmd = updateSettingsKey(m, key("enter"))
	require.True(t, m.mouseOn)
	require.Equal(t, []tea.Msg{enableMouseMsg}, cmdMsgs(cmd))

	// r resets to the default (on): already on, nothing to emit.
	m, cmd = updateSettingsKey(m, key("r"))
	require.True(t, m.mouseOn)
	require.Nil(t, cmd)

	// Unrelated keys never touch capture.
	_, cmd = updateSettingsKey(m, key("j"))
	require.Nil(t, cmd)
}

// tea.ExecProcess (native attach) turns mouse reporting off when it hands
// the terminal over; coming back must turn it on again.
func TestAttachDone_RestoresMouse(t *testing.T) {
	m := boardModel()
	m.mouseOn = true
	mi, cmd := m.Update(attachDoneMsg{})
	m = mi.(Model)
	require.True(t, m.mouseOn)
	require.Equal(t, []tea.Msg{enableMouseMsg}, cmdMsgs(cmd))
}

// The popup shares the board's capture: opening it while the board holds the
// mouse emits nothing new, and detaching keeps it (TestDetach_MouseFollowsBoard).
func TestOpenAttach_SharesBoardCapture(t *testing.T) {
	m := boardModel()
	m.mouseOn = true
	m, cmd := syncMouse(m, "attach")
	require.Nil(t, cmd)
	require.True(t, m.mouseOn)
}

// renderedRowAt returns the session whose slug the rendered screen line
// shows, or "" when the line is not a session row.
func renderedRowAt(m Model, line string) string {
	for _, s := range m.Sessions {
		if strings.Contains(line, " "+s.Slug+" ") {
			return s.ID
		}
	}
	return ""
}

// Click every screen line of the real View() across header / banner /
// density / scroll variants: a click selects exactly the row drawn there and
// does nothing elsewhere.
func TestBoardMouse_ClickEveryRenderedRow(t *testing.T) {
	for _, density := range []string{"compact", "normal", "roomy"} {
		for _, banner := range []bool{false, true} {
			for _, scrolled := range []bool{false, true} {
				name := fmt.Sprintf("%s/banner=%v/scrolled=%v", density, banner, scrolled)
				t.Run(name, func(t *testing.T) {
					m := scrollModel(t, 30, density, 22)
					m.BackendUnavailable = banner
					rows := orderedSessions(m)
					if scrolled {
						m.SelectedID = rows[len(rows)-1].ID
					} else {
						m.SelectedID = rows[0].ID
					}
					m = ensureVisible(m)
					if scrolled {
						require.NotZero(t, m.ScrollOffset)
					}
					screen := strings.Split(ansi.Strip(m.View()), "\n")
					require.Len(t, screen, m.Height)
					hits := 0
					for y, line := range screen {
						want := renderedRowAt(m, line)
						got, _ := updateMouse(m, click(40, y))
						if want == "" {
							require.Equal(t, m.SelectedID, got.SelectedID, "line %d %q must not select", y, line)
							continue
						}
						hits++
						require.Equal(t, want, got.SelectedID, "line %d %q", y, line)
						require.Equal(t, m.ScrollOffset, got.ScrollOffset, "clicking a visible row must not scroll")
					}
					require.Greater(t, hits, 5)
				})
			}
		}
	}
}

func TestBoardMouse_DoubleClickOpens(t *testing.T) {
	m := scrollModel(t, 6, "normal", 30)
	now := time.Unix(1000, 0)
	m.Clock = func() time.Time { return now }
	y := boardTop(m) + 1 // first row under "Needs input"
	id := sessionAtScreenRow(m, y)
	require.NotEmpty(t, id)

	m, _ = updateMouse(m, click(40, y))
	require.Equal(t, id, m.SelectedID)
	require.Empty(t, m.Err)

	// Second click inside the window opens (no socket here, so the attach
	// path reports that — proof it ran).
	now = now.Add(100 * time.Millisecond)
	m2, _ := updateMouse(m, click(40, y))
	require.Equal(t, "attach: socket unknown", m2.Err)

	// Too slow: just a re-select.
	now = now.Add(time.Second)
	m3, _ := updateMouse(m, click(40, y))
	require.Empty(t, m3.Err)

	// A quick click on a different row is a new single click.
	other := sessionAtScreenRow(m, y+1)
	require.NotEmpty(t, other)
	now = now.Add(50 * time.Millisecond)
	m4, _ := updateMouse(m3, click(40, y+1))
	require.Equal(t, other, m4.SelectedID)
	require.Empty(t, m4.Err)
}

// The wheel moves the selection like j/k (silently) and the board scrolls
// with it.
func TestBoardMouse_WheelMovesSelection(t *testing.T) {
	m := scrollModel(t, 30, "normal", 20)
	rec := &recordingAudio{}
	m.Audio = rec
	rows := orderedSessions(m)
	m.SelectedID = rows[0].ID
	m = ensureVisible(m)

	for i := 1; i < len(rows); i++ {
		m, _ = updateMouse(m, wheel(true))
		require.Equal(t, rows[i].ID, m.SelectedID)
		require.True(t, arrowVisible(m), "step %d scroll %d", i, m.ScrollOffset)
	}
	require.NotZero(t, m.ScrollOffset)
	m, _ = updateMouse(m, wheel(true))
	require.Equal(t, rows[len(rows)-1].ID, m.SelectedID, "clamped at the end")

	m, _ = updateMouse(m, wheel(false))
	require.Equal(t, rows[len(rows)-2].ID, m.SelectedID)
	require.Empty(t, rec.Events, "wheel is silent")

	// Keyboard and wheel agree.
	k := moveSelection(m, -1)
	w, _ := updateMouse(m, wheel(false))
	require.Equal(t, k.SelectedID, w.SelectedID)
	require.Equal(t, k.ScrollOffset, w.ScrollOffset)
}

func TestBoardMouse_IgnoredWhenDisabled(t *testing.T) {
	m := scrollModel(t, 6, "normal", 30)
	m.Store = mouseStore(t, false)
	sel := m.SelectedID
	y := boardTop(m) + 1
	require.NotEmpty(t, sessionAtScreenRow(m, y))
	m, cmd := updateMouse(m, click(40, y))
	require.Nil(t, cmd)
	require.Equal(t, sel, m.SelectedID)
	m, _ = updateMouse(m, wheel(true))
	require.Equal(t, sel, m.SelectedID)
}

// Overlays and bottom-bar prompts own input: mouse events must not reach the
// board underneath.
func TestBoardMouse_OverlaysDoNotLeak(t *testing.T) {
	foci := map[string]Focus{
		"help": FocusHelp, "stats": FocusStats, "wizard": FocusWizard, "fail": FocusFail,
		"confirm-delete": FocusConfirmDelete, "confirm-quit": FocusConfirmQuit,
		"prompt": FocusPrompt, "command": FocusCommand, "rename": FocusRename,
		"settings": FocusSettings, "boot": FocusBoot,
	}
	for name, f := range foci {
		t.Run(name, func(t *testing.T) {
			m := scrollModel(t, 30, "normal", 20)
			m.Settings = &SettingsState{CursorID: settings.Registry[0].ID}
			rows := orderedSessions(m)
			m.SelectedID = rows[0].ID
			m = ensureVisible(m)
			m.Focus = f
			before := m
			for y := 0; y < m.Height; y++ {
				m, _ = updateMouse(m, click(40, y))
			}
			m, _ = updateMouse(m, wheel(true))
			require.Equal(t, before.SelectedID, m.SelectedID)
			require.Equal(t, before.ScrollOffset, m.ScrollOffset)
			require.Equal(t, before.Filter, m.Filter)
			require.Equal(t, f, m.Focus)
		})
	}
}

func TestSettingsMouse_WheelMovesCursor(t *testing.T) {
	m := scrollModel(t, 3, "normal", 30)
	m.Focus = FocusSettings
	first := settings.Registry[0].ID
	m.Settings = &SettingsState{CursorID: first}
	m, _ = updateMouse(m, wheel(true))
	require.Equal(t, settingsAfter(first, 1), m.Settings.CursorID)
	m, _ = updateMouse(m, wheel(false))
	require.Equal(t, first, m.Settings.CursorID)
}

// headerX finds needle's cell column in rendered screen line y.
func headerX(t *testing.T, m Model, y int, needle string) int {
	t.Helper()
	line := strings.Split(ansi.Strip(m.View()), "\n")[y]
	lo, _, _, ok := cellSpan(line, needle, 0)
	require.True(t, ok, "%q not on line %d: %q", needle, y, line)
	return lo
}

func TestBoardMouse_FilterChipClick(t *testing.T) {
	for _, banner := range []bool{false, true} {
		m := scrollModel(t, 30, "normal", 30)
		m.BackendUnavailable = banner
		for _, tool := range []string{"claude", "codex", "gemini", "ollama", "all"} {
			x := headerX(t, m, 2, " "+tool+" ") + 1
			for _, dx := range []int{0, len(tool) - 1} {
				got, _ := updateMouse(m, click(x+dx, 2))
				require.Equal(t, tool, got.Filter, "banner=%v click %q at x=%d", banner, tool, x+dx)
			}
			m, _ = updateMouse(m, click(x, 2))
			for _, s := range orderedSessions(m) {
				require.True(t, tool == "all" || s.ToolID == tool)
			}
		}
		// The separators are not chips.
		x := headerX(t, m, 2, "·")
		got, _ := updateMouse(m, click(x, 2))
		require.Equal(t, m.Filter, got.Filter)
	}
}

func TestBoardMouse_NewAgentClickOpensWizard(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // no tools.yaml: openWizard reports it
	m := scrollModel(t, 3, "normal", 30)
	x := headerX(t, m, 1, "+ new agent")
	got, _ := updateMouse(m, click(x+3, 1))
	require.True(t, got.Focus == FocusWizard || strings.HasPrefix(got.Err, "wizard:"), "focus %v err %q", got.Focus, got.Err)

	// Elsewhere on the top row does nothing.
	got, _ = updateMouse(m, click(x-2, 1))
	require.Equal(t, FocusBoard, got.Focus)
	require.Empty(t, got.Err)
}
