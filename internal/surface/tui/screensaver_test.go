package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// fakeLauncher stands in for caffeinate: it counts starts and stops and
// never runs a process.
type fakeLauncher struct {
	starts, stops int
	err           error
}

func (f *fakeLauncher) launch() (func(), error) {
	if f.err != nil {
		return nil, f.err
	}
	f.starts++
	return func() { f.stops++ }, nil
}

// saverModel is a board with default settings, a fake clock, a live idle
// timer channel and a fake keep-awake. Advance the clock through the
// returned pointer.
func saverModel(t *testing.T, sessions ...protocol.SessionSummary) (Model, *time.Time, *fakeLauncher) {
	t.Helper()
	m := boardModel(sessions...)
	m.Store = settings.NewStore()
	m.StorePath = filepath.Join(t.TempDir(), "config.yaml")
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	m.Clock = func() time.Time { return now }
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	m.idleDone = done
	fl := &fakeLauncher{}
	m.keepAwake = &displayAwake{launch: fl.launch}
	m.lastInput = now
	return m, &now, fl
}

// step runs one message through the real Update.
func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	nm, ok := next.(Model)
	require.True(t, ok)
	return nm, cmd
}

// idleTo moves the clock to start+d and delivers the pending idle tick.
func idleTo(t *testing.T, m Model, now *time.Time, start time.Time, d time.Duration) Model {
	t.Helper()
	*now = start.Add(d)
	m, _ = step(t, m, idleTickMsg{gen: m.idleGen})
	return m
}

func saverSessions() []protocol.SessionSummary {
	return []protocol.SessionSummary{
		{ID: "a", Slug: "alpha", ToolID: "claude", State: protocol.StateNeedsInput},
		{ID: "b", Slug: "beta", ToolID: "claude", State: protocol.StateWorking},
		{ID: "c", Slug: "gamma", ToolID: "claude", State: protocol.StateQueued},
		{ID: "d", Slug: "delta", ToolID: "claude", State: protocol.StateDone},
		{ID: "e", Slug: "eps", ToolID: "claude", State: protocol.StateFailed},
		{ID: "f", Slug: "zeta", ToolID: "claude", State: protocol.StateCrashed,
			RetireAt: time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC)}, // marked done: counts as done
	}
}

func TestScreensaverShowsAfterIdleMinutes(t *testing.T) {
	m, now, fl := saverModel(t, saverSessions()...)
	start := *now

	m = idleTo(t, m, now, start, 5*time.Minute-time.Second)
	require.False(t, m.saverOn, "not before screensaver_after_minutes")
	require.Zero(t, fl.starts)

	m = idleTo(t, m, now, start, 5*time.Minute)
	require.True(t, m.saverOn)
	require.Equal(t, start.Add(5*time.Minute), m.saverSince)
	require.Equal(t, 1, fl.starts, "keep-awake starts with the screensaver")
	require.True(t, m.keepAwake.holding())
	require.Contains(t, ansi.Strip(m.View()), saverLogo)
}

func TestScreensaverHonorsAfterMinutesSetting(t *testing.T) {
	m, now, _ := saverModel(t)
	require.NoError(t, m.Store.Set("screensaver_after_minutes", 1))
	start := *now
	m = idleTo(t, m, now, start, 59*time.Second)
	require.False(t, m.saverOn)
	m = idleTo(t, m, now, start, time.Minute)
	require.True(t, m.saverOn)
}

func TestScreensaverInputResetsIdleTime(t *testing.T) {
	m, now, _ := saverModel(t, saverSessions()...)
	start := *now
	*now = start.Add(4 * time.Minute)
	m, _ = step(t, m, key("j"))
	m = idleTo(t, m, now, start, 6*time.Minute)
	require.False(t, m.saverOn, "a key at 4m restarts the 5m countdown")
	*now = start.Add(8*time.Minute + 59*time.Second)
	m, _ = step(t, m, tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	m = idleTo(t, m, now, start, 10*time.Minute)
	require.False(t, m.saverOn, "a mouse event restarts it too")
	m = idleTo(t, m, now, start, 14*time.Minute)
	require.True(t, m.saverOn)
}

func TestScreensaverDisabled(t *testing.T) {
	m, now, fl := saverModel(t)
	require.NoError(t, m.Store.Set("screensaver_enabled", false))
	m = idleTo(t, m, now, *now, time.Hour)
	require.False(t, m.saverOn)
	require.Zero(t, fl.starts)
}

// Whatever has focus, an idle screen gives way to the screensaver; waking
// returns to that focus with its state, and the waking key reaches nothing.
func TestScreensaverUnderEveryFocus(t *testing.T) {
	cases := map[string]func(Model) Model{
		"prompt":         func(m Model) Model { m.Focus = FocusPrompt; return m },
		"command":        func(m Model) Model { m.Focus = FocusCommand; return m },
		"search":         func(m Model) Model { m.Focus = FocusSearch; return m },
		"help":           func(m Model) Model { m.Focus = FocusHelp; return m },
		"stats":          func(m Model) Model { m.Focus = FocusStats; return m },
		"overview":       func(m Model) Model { m.Focus = FocusOverview; return m },
		"confirm quit":   func(m Model) Model { m.Focus = FocusConfirmQuit; return m },
		"confirm delete": func(m Model) Model { m.Focus = FocusConfirmDelete; return m },
		"settings": func(m Model) Model {
			m.Focus, m.Settings = FocusSettings, &SettingsState{}
			return m
		},
		"wizard": func(m Model) Model {
			m.Focus, m.Wizard = FocusWizard, &WizardState{}
			return m
		},
		"rename": func(m Model) Model {
			m.Focus, m.Rename = FocusRename, &RenameState{SessionID: "a"}
			return m
		},
		"failures": func(m Model) Model {
			m.Focus, m.Fail = FocusFail, &FailState{}
			return m
		},
		"attach": func(m Model) Model {
			m.Focus, m.Attach = FocusAttach, &AttachState{SessionID: "a"}
			return m
		},
		"command over attach": func(m Model) Model {
			m.Focus, m.Attach = FocusCommand, &AttachState{SessionID: "a"}
			return m
		},
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			m, now, fl := saverModel(t, saverSessions()...)
			m = set(m)
			focus := m.Focus
			m = idleTo(t, m, now, *now, 5*time.Minute)
			require.True(t, m.saverOn)
			require.Equal(t, focus, m.Focus, "the focused component stays underneath")
			require.Equal(t, 1, fl.starts)
			require.Contains(t, ansi.Strip(m.View()), saverLogo)

			m, _ = step(t, m, key("x"))
			require.False(t, m.saverOn)
			require.Equal(t, focus, m.Focus, "waking returns to the same focus")
			require.Equal(t, 1, fl.stops)
			require.Empty(t, m.PromptText+m.CmdText, "the waking key typed nothing")
		})
	}
}

// The new-session wizard left open is the reported case: it used to block
// the screensaver (and kept the cursor blinking at 10 FPS) forever.
func TestScreensaverOverOpenWizard(t *testing.T) {
	m, now, _ := saverModel(t, saverSessions()...)
	m.Focus, m.Wizard = FocusWizard, &WizardState{}
	require.True(t, m.wantsSpinnerTick(), "the wizard cursor blinks")
	m = idleTo(t, m, now, *now, 5*time.Minute)
	require.True(t, m.saverOn)
	require.False(t, m.wantsSpinnerTick(), "no cursor ticks under the screensaver")
	m, _ = step(t, m, key("esc"))
	require.Equal(t, FocusWizard, m.Focus, "esc only woke; the wizard is still open")
	require.NotNil(t, m.Wizard)
}

func TestScreensaverResizeWakes(t *testing.T) {
	m, now, fl := saverModel(t, saverSessions()...)
	start := *now
	m = idleTo(t, m, now, start, 5*time.Minute)
	require.True(t, m.saverOn)
	*now = start.Add(6 * time.Minute)
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 90, Height: 30})
	require.False(t, m.saverOn)
	require.Equal(t, 1, fl.stops)
	require.Equal(t, *now, m.lastInput, "a resize restarts the countdown")
	m = idleTo(t, m, now, start, 10*time.Minute)
	require.False(t, m.saverOn)
	m = idleTo(t, m, now, start, 11*time.Minute)
	require.True(t, m.saverOn)
}

func TestScreensaverResizeDuringBootIsNotInput(t *testing.T) {
	m, _, _ := saverModel(t)
	m.Focus = FocusBoot
	m.lastInput = time.Time{}
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 90, Height: 30})
	require.True(t, m.lastInput.IsZero())
}

// Turning the setting off (a live reload) while it shows hides it at the
// next idle tick.
func TestScreensaverDisabledWhileShowing(t *testing.T) {
	m, now, fl := saverModel(t, saverSessions()...)
	m = idleTo(t, m, now, *now, 5*time.Minute)
	require.True(t, m.saverOn)
	require.NoError(t, m.Store.Set("screensaver_enabled", false))
	m = idleTo(t, m, now, *now, time.Minute)
	require.False(t, m.saverOn)
	require.Equal(t, 1, fl.stops)
}

func TestScreensaverNotDuringBoot(t *testing.T) {
	m, now, _ := saverModel(t)
	m.Focus = FocusBoot
	m, cmd := step(t, m, idleTickMsg{gen: m.idleGen})
	require.Nil(t, cmd, "no idle timer during the boot splash")
	require.False(t, m.idleArmed)
	*now = now.Add(time.Hour)
	require.False(t, checkIdle(m, *now).saverOn)
}

func TestScreensaverKeyWakesWithoutActing(t *testing.T) {
	for _, k := range []tea.KeyMsg{
		key("j"), key("enter"), key(":"), key("?"), key("S"), key("q"), key("i"),
		{Type: tea.KeyCtrlC}, key("esc"),
	} {
		t.Run(k.String(), func(t *testing.T) {
			m, now, fl := saverModel(t, saverSessions()...)
			m = idleTo(t, m, now, *now, 5*time.Minute)
			require.True(t, m.saverOn)

			*now = now.Add(time.Hour)
			m, cmd := step(t, m, k)
			require.False(t, m.saverOn)
			require.Equal(t, FocusBoard, m.Focus, "the waking key opened nothing")
			require.Equal(t, "a", m.SelectedID, "the waking key moved nothing")
			require.False(t, m.Quitting)
			require.Nil(t, m.Attach)
			require.Equal(t, *now, m.lastInput, "waking restarts the idle countdown")
			require.Equal(t, 1, fl.stops, "keep-awake stops with the screensaver")
			require.False(t, m.keepAwake.holding())
			require.Contains(t, ansi.Strip(m.View()), "READY", "the board is back")
			_ = cmd
		})
	}
}

func TestScreensaverNextKeyActsAfterWaking(t *testing.T) {
	m, now, _ := saverModel(t, saverSessions()...)
	m = idleTo(t, m, now, *now, 5*time.Minute)
	m, _ = step(t, m, key("j"))
	require.Equal(t, "a", m.SelectedID)
	m, _ = step(t, m, key("j"))
	require.Equal(t, "b", m.SelectedID)
}

// A chord half-typed before the screensaver must not finish with the key
// that wakes it, nor with the one after.
func TestScreensaverClearsPendingChord(t *testing.T) {
	m, now, _ := saverModel(t, saverSessions()...)
	m, _ = step(t, m, key("d"))
	require.Equal(t, "d", m.PendingChord)
	m = idleTo(t, m, now, *now, 6*time.Minute)
	require.True(t, m.saverOn)
	m, _ = step(t, m, key("d"))
	m, _ = step(t, m, key("d"))
	require.Equal(t, FocusBoard, m.Focus, "no delete confirmation")
	require.Equal(t, "d", m.PendingChord)
}

func TestScreensaverMouseWakesWithoutActing(t *testing.T) {
	m, now, fl := saverModel(t, saverSessions()...)
	rowB := -1
	for y := 0; y < m.Height; y++ {
		if sessionAtScreenRow(m, y) == "b" {
			rowB = y
		}
	}
	require.GreaterOrEqual(t, rowB, 0)
	click := tea.MouseMsg{X: 10, Y: rowB, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}

	m = idleTo(t, m, now, *now, 5*time.Minute)
	m, _ = step(t, m, click)
	require.False(t, m.saverOn)
	require.Equal(t, "a", m.SelectedID, "the waking click selected nothing")
	require.Equal(t, 1, fl.stops)

	m, _ = step(t, m, click)
	require.Equal(t, "b", m.SelectedID, "the next click acts")

	m = idleTo(t, m, now, *now, 5*time.Minute)
	require.True(t, m.saverOn)
	m, _ = step(t, m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	require.False(t, m.saverOn)
	require.Equal(t, "b", m.SelectedID, "the waking wheel moved nothing")
}

func TestScreensaverCountsFollowDaemonEvents(t *testing.T) {
	m, now, _ := saverModel(t, saverSessions()...)
	m = idleTo(t, m, now, *now, 5*time.Minute)
	require.Contains(t, ansi.Strip(m.View()), "1 ready · 2 working · 2 done · 1 failed")

	m, _ = step(t, m, DaemonEventMsg{Env: stateUpdate(t, "b", protocol.StateNeedsInput)})
	m, _ = step(t, m, DaemonEventMsg{Env: stateUpdate(t, "c", protocol.StateDone)})
	require.True(t, m.saverOn, "daemon events don't wake it")
	require.Contains(t, ansi.Strip(m.View()), "2 ready · 0 working · 3 done · 1 failed")
}

// The screensaver's numbers are the header's.
func TestScreensaverCountsMatchHeader(t *testing.T) {
	m := boardModel(saverSessions()...)
	require.Contains(t, ansi.Strip(renderHeader(m, 200)), "1 ready · 2 working · 2 done  · 1 failed")
	require.Equal(t, "1 ready · 2 working · 2 done · 1 failed", ansi.Strip(saverCounts(m)))

	m = boardModel()
	require.Equal(t, "0 ready · 0 working · 0 done", ansi.Strip(saverCounts(m)))
}

func TestScreensaverHighlightsNeedsYouOnlyWhenNonZero(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	m := boardModel(saverSessions()...)
	require.Contains(t, saverCounts(m), styleAlert.Render("1 ready"))
	require.Contains(t, saverCounts(m), styleStateFailed.Render("1 failed"))

	m = boardModel(protocol.SessionSummary{ID: "b", State: protocol.StateWorking})
	require.NotContains(t, saverCounts(m), styleAlert.Render("0 ready"))
	require.Contains(t, saverCounts(m), styleHeaderMeta.Render("0 ready"))
}

func TestScreensaverKeepAwakeSetting(t *testing.T) {
	m, now, fl := saverModel(t)
	require.NoError(t, m.Store.Set("screensaver_keep_awake", false))
	m = idleTo(t, m, now, *now, 5*time.Minute)
	require.True(t, m.saverOn)
	require.Zero(t, fl.starts, "keep-awake off: no helper")
	m, _ = step(t, m, key("x"))
	require.False(t, m.saverOn)
	require.Zero(t, fl.stops)
}

func TestScreensaverRestsTheSpinner(t *testing.T) {
	m, now, _ := saverModel(t, saverSessions()...)
	require.True(t, m.wantsSpinnerTick(), "a working session spins on the board")
	m = idleTo(t, m, now, *now, 5*time.Minute)
	require.True(t, m.saverOn)
	require.False(t, m.wantsSpinnerTick(), "no fast ticks under the screensaver")
	m.spinnerRunning = false
	_, cmd := armSpinner(m)
	require.Nil(t, cmd)
}

func TestScreensaverSkipsDescriptionAnimationTicks(t *testing.T) {
	m, now, _ := saverModel(t, saverSessions()...)
	m = idleTo(t, m, now, *now, 5*time.Minute)
	m, _ = step(t, m, DaemonEventMsg{Env: retireUpdate(t, "b", map[string]any{"description": "editing styles"})})
	require.Len(t, m.DescAnim, 1, "the animation is registered")
	require.False(t, m.descTickPending, "but no 30 FPS tick runs under the screensaver")

	m, _ = step(t, m, key("x"))
	require.Empty(t, m.DescAnim, "waking shows the end state")
	require.Contains(t, ansi.Strip(m.View()), "beta")
}

// Update keeps exactly one idle timer pending, and only for a model that
// has the program's idleDone.
func TestIdleTimerArming(t *testing.T) {
	m, _, _ := saverModel(t)
	m, cmd := step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	require.NotNil(t, cmd)
	require.True(t, m.idleArmed)
	gen := m.idleGen

	m, _ = step(t, m, key("j"))
	require.Equal(t, gen, m.idleGen, "input doesn't stack timers")

	m, _ = step(t, m, idleTickMsg{gen: gen - 1})
	require.Equal(t, gen, m.idleGen, "a stale tick is ignored")
	require.True(t, m.idleArmed)

	m, cmd = step(t, m, idleTickMsg{gen: gen})
	require.NotNil(t, cmd)
	require.True(t, m.idleArmed)
	require.Equal(t, gen+1, m.idleGen, "a tick re-arms the next one")

	plain := boardModel()
	plain, cmd = step(t, plain, key("j"))
	require.Nil(t, cmd, "no idleDone: no timer")
	require.False(t, plain.idleArmed)
}

func TestIdleDelay(t *testing.T) {
	m, now, _ := saverModel(t)
	start := *now
	require.Equal(t, idleMaxWait, idleDelay(m, start), "5m away: capped at a minute")
	require.Equal(t, 30*time.Second, idleDelay(m, start.Add(4*time.Minute+30*time.Second)))
	require.Equal(t, idleMinWait, idleDelay(m, start.Add(5*time.Minute-time.Millisecond)))
	require.Equal(t, idleMaxWait, idleDelay(m, start.Add(time.Hour)), "past due over an overlay: check again in a minute")

	m.saverOn, m.saverSince = true, start
	require.Equal(t, 40*time.Second, idleDelay(m, start.Add(80*time.Second)), "next drift step")
	require.NoError(t, m.Store.Set("reduce_motion", true))
	require.Equal(t, idleMaxWait, idleDelay(m, start.Add(80*time.Second)))

	m.saverOn = false
	require.NoError(t, m.Store.Set("screensaver_enabled", false))
	require.Equal(t, idleMaxWait, idleDelay(m, start))
}

func TestIdleTickCmd(t *testing.T) {
	done := make(chan struct{})
	close(done)
	require.Nil(t, idleTick(1, time.Hour, done)(), "exit ends a pending timer")

	live := make(chan struct{})
	defer close(live)
	require.Equal(t, idleTickMsg{gen: 7}, idleTick(7, time.Millisecond, live)())
}

func TestHandOffStartsIdleTime(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	m := Model{Focus: FocusBoot, Clock: func() time.Time { return now }}
	m, _ = m.handOffToBoard()
	require.Equal(t, now, m.lastInput)
}

func TestScreensaverView(t *testing.T) {
	sizes := []struct{ w, h int }{{80, 24}, {200, 60}, {40, 10}, {30, 5}, {20, 3}, {12, 2}, {6, 1}}
	for _, sz := range sizes {
		m, now, _ := saverModel(t, saverSessions()...)
		m.Width, m.Height = sz.w, sz.h
		m = idleTo(t, m, now, *now, 5*time.Minute)
		require.True(t, m.saverOn)

		lines := strings.Split(m.View(), "\n")
		require.Len(t, lines, sz.h, "%dx%d: exactly the terminal height", sz.w, sz.h)
		for i, l := range lines {
			require.Equal(t, sz.w, ansi.StringWidth(l), "%dx%d line %d: exactly the width, never wrapped", sz.w, sz.h, i)
		}
		text := ansi.Strip(m.View())
		if sz.h >= 2 && sz.w >= ansi.StringWidth(saverLogo) {
			require.Contains(t, text, saverLogo, "%dx%d", sz.w, sz.h)
		}
		require.Contains(t, text, "1 ", "%dx%d: the counts always show", sz.w, sz.h)
	}
}

func TestScreensaverViewCentered(t *testing.T) {
	m, now, _ := saverModel(t, saverSessions()...)
	m.Width, m.Height = 80, 24
	m = idleTo(t, m, now, *now, 5*time.Minute)

	lines := strings.Split(ansi.Strip(m.View()), "\n")
	counts := "1 ready · 2 working · 2 done · 1 failed"
	top := (24 - 3) / 2
	require.Equal(t, strings.Repeat(" ", (80-5)/2)+saverLogo, strings.TrimRight(lines[top], " "))
	require.Empty(t, strings.TrimSpace(lines[top+1]))
	require.Equal(t, strings.Repeat(" ", (80-ansi.StringWidth(counts))/2)+counts, strings.TrimRight(lines[top+2], " "))
	for i, l := range lines {
		if i < top || i > top+2 {
			require.Empty(t, strings.TrimSpace(l), "line %d", i)
		}
	}
}

// saverOrigin returns the row and column of the logo on screen.
func saverOrigin(t *testing.T, m Model) (row, col int) {
	t.Helper()
	for i, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if c := strings.Index(l, saverLogo); c >= 0 {
			return i, c
		}
	}
	t.Fatal("no logo on screen")
	return 0, 0
}

func TestScreensaverDriftsSlowly(t *testing.T) {
	m, now, _ := saverModel(t, saverSessions()...)
	m.Width, m.Height = 80, 24
	m = idleTo(t, m, now, *now, 5*time.Minute)
	shown := *now
	row0, col0 := saverOrigin(t, m)

	*now = shown.Add(59 * time.Second)
	r, c := saverOrigin(t, m)
	require.Equal(t, [2]int{row0, col0}, [2]int{r, c}, "still within the first minute")

	moved := false
	for minute := 1; minute <= 30; minute++ {
		*now = shown.Add(time.Duration(minute) * time.Minute)
		r, c := saverOrigin(t, m)
		require.LessOrEqual(t, abs(r-row0), saverDriftY, "minute %d", minute)
		require.LessOrEqual(t, abs(c-col0), saverDriftX, "minute %d", minute)
		if r != row0 || c != col0 {
			moved = true
		}
	}
	require.True(t, moved, "it wanders over half an hour")
}

func TestScreensaverReduceMotionStaysCentered(t *testing.T) {
	m, now, _ := saverModel(t, saverSessions()...)
	require.NoError(t, m.Store.Set("reduce_motion", true))
	m.Width, m.Height = 80, 24
	m = idleTo(t, m, now, *now, 5*time.Minute)
	shown := *now
	row0, col0 := saverOrigin(t, m)
	for minute := 1; minute <= 30; minute++ {
		*now = shown.Add(time.Duration(minute) * time.Minute)
		r, c := saverOrigin(t, m)
		require.Equal(t, [2]int{row0, col0}, [2]int{r, c}, "minute %d", minute)
	}
}

func TestScreensaverDriftStaysOnSmallScreens(t *testing.T) {
	m, now, _ := saverModel(t, saverSessions()...)
	m.Width, m.Height = 44, 3 // counts are 42 wide: one column of slack
	m = idleTo(t, m, now, *now, 5*time.Minute)
	shown := *now
	for minute := 0; minute <= 30; minute++ {
		*now = shown.Add(time.Duration(minute) * time.Minute)
		lines := strings.Split(m.View(), "\n")
		require.Len(t, lines, 3)
		require.Contains(t, ansi.Strip(m.View()), "1 ready · 2 working · 2 done · 1 failed", "minute %d: never cut", minute)
	}
}

func TestScreensaverShowsReconnect(t *testing.T) {
	m, now, _ := saverModel(t, saverSessions()...)
	m = idleTo(t, m, now, *now, 5*time.Minute)
	m.reconnect = &reconnectState{}
	require.Contains(t, ansi.Strip(m.View()), reconnectStatus, "stale counts are flagged")
}

func TestRunProgramReleasesKeepAwake(t *testing.T) {
	fl := &fakeLauncher{}
	awake := &displayAwake{launch: fl.launch}
	done := make(chan struct{})
	err := runProgram(func() (tea.Model, error) {
		awake.Start() // the TUI exits while the screensaver shows
		return Model{}, nil
	}, awake, done)
	require.NoError(t, err)
	require.Equal(t, 1, fl.stops)
	require.False(t, awake.holding())
	_, open := <-done
	require.False(t, open, "the idle timer channel is closed")

	boom := errors.New("boom")
	fl2 := &fakeLauncher{}
	awake2 := &displayAwake{launch: fl2.launch}
	err = runProgram(func() (tea.Model, error) {
		awake2.Start()
		return nil, boom
	}, awake2, make(chan struct{}))
	require.ErrorIs(t, err, boom)
	require.Equal(t, 1, fl2.stops, "released on error too")

	bootErr := errors.New("no daemon")
	err = runProgram(func() (tea.Model, error) {
		return Model{BootFailed: true, BootError: bootErr}, nil
	}, nil, make(chan struct{}))
	require.ErrorIs(t, err, bootErr)
}

// The settings page edits the three settings like any bool / int.
func TestScreensaverSettingsPage(t *testing.T) {
	m := Model{
		Store:     settings.NewStore(),
		StorePath: filepath.Join(t.TempDir(), "config.yaml"),
		Settings:  &SettingsState{CursorID: "screensaver_enabled"},
	}
	m = applySettingsAction(m, 0)
	require.Equal(t, false, m.Store.Get("screensaver_enabled"))
	m = applySettingsAction(m, +1)
	require.Equal(t, true, m.Store.Get("screensaver_enabled"))

	m.Settings.CursorID = "screensaver_after_minutes"
	m = applySettingsAction(m, +1)
	require.Equal(t, 6, m.Store.Get("screensaver_after_minutes"))
	for range 10 {
		m = applySettingsAction(m, -1)
	}
	require.Equal(t, 1, m.Store.Get("screensaver_after_minutes"), "clamped at 1")

	m.Settings.CursorID = "screensaver_keep_awake"
	m = applySettingsAction(m, -1)
	require.Equal(t, false, m.Store.Get("screensaver_keep_awake"))

	m.Height = 60
	page := ansi.Strip(renderSettings(m, 100))
	for _, label := range []string{"Screensaver ", "Screensaver after (min)", "Screensaver keep awake"} {
		require.Contains(t, page, label)
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// saverAt shows the screensaver on a w×h terminal with the given settings.
func saverAt(t *testing.T, w, h int, kv ...any) Model {
	t.Helper()
	m, now, _ := saverModel(t, saverSessions()...)
	for i := 0; i < len(kv); i += 2 {
		id, ok := kv[i].(string)
		require.True(t, ok)
		require.NoError(t, m.Store.Set(id, kv[i+1]))
	}
	m.Width, m.Height = w, h
	m = idleTo(t, m, now, *now, 5*time.Minute)
	require.True(t, m.saverOn)
	return m
}

// saverLines is the screensaver's non-blank lines, right-trimmed, and the
// row of the first one.
func saverLines(m Model) (lines []string, top int) {
	top = -1
	for i, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if top < 0 {
			top = i
		}
		lines = append(lines, strings.TrimRight(l, " "))
	}
	return lines, top
}

// vertical: one count per line under the logo, the numbers in one column
// and the block centered as a unit.
func TestScreensaverCountsVertical(t *testing.T) {
	m := saverAt(t, 80, 24, "screensaver_counts", "vertical", "reduce_motion", true)
	block := []string{"1 ready", "2 working", "2 done", "1 failed"}
	pad := strings.Repeat(" ", (80-9)/2) // 9 = "2 working"
	want := []string{strings.Repeat(" ", (80-5)/2) + saverLogo}
	for _, l := range block {
		want = append(want, pad+l)
	}
	lines, top := saverLines(m)
	require.Equal(t, want, lines)
	require.Equal(t, (24-6)/2, top, "logo, blank, four counts: centered")
	all := strings.Split(ansi.Strip(m.View()), "\n")
	require.Empty(t, strings.TrimSpace(all[top+1]), "a blank between logo and counts")
}

func TestScreensaverCountsVerticalAlignsNumbers(t *testing.T) {
	var ss []protocol.SessionSummary
	for i := range 12 {
		ss = append(ss, protocol.SessionSummary{ID: fmt.Sprint(i), State: protocol.StateDone})
	}
	ss = append(ss, protocol.SessionSummary{ID: "w", State: protocol.StateWorking})
	m := boardModel(ss...)
	require.Equal(t, []string{" 0 ready  ", " 1 working", "12 done   "}, stripAll(saverCountLines(m, "vertical")))
}

func stripAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = ansi.Strip(s)
	}
	return out
}

func TestScreensaverCountsVerticalKeepsStyles(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })

	lines := saverCountLines(boardModel(saverSessions()...), "vertical")
	require.Contains(t, lines[0], styleAlert.Render("1 ready"))
	require.Contains(t, lines[3], styleStateFailed.Render("1 failed"))
}

// A terminal too short for one count per line gets today's single line.
func TestScreensaverCountsVerticalFallsBackWhenShort(t *testing.T) {
	m := saverAt(t, 80, 4, "screensaver_counts", "vertical")
	text := ansi.Strip(m.View())
	require.Contains(t, text, saverLogo)
	require.Contains(t, text, "1 ready · 2 working · 2 done · 1 failed")

	m = saverAt(t, 80, 5, "screensaver_counts", "vertical", "reduce_motion", true)
	lines, _ := saverLines(m)
	require.Len(t, lines, 5, "logo and four counts, no blank, fit exactly")
	require.Contains(t, lines[1], "1 ready")
}

func TestScreensaverCountsOff(t *testing.T) {
	m := saverAt(t, 80, 24, "screensaver_counts", "off", "reduce_motion", true)
	lines, top := saverLines(m)
	require.Equal(t, []string{strings.Repeat(" ", (80-5)/2) + saverLogo}, lines)
	require.Equal(t, (24-1)/2, top)

	m.reconnect = &reconnectState{}
	require.Contains(t, ansi.Strip(m.View()), reconnectStatus, "the reconnect status still shows")
}

func TestScreensaverClock(t *testing.T) {
	m := saverAt(t, 80, 24, "screensaver_clock", "24h", "reduce_motion", true)
	lines, _ := saverLines(m)
	require.Len(t, lines, 3)
	require.Equal(t, "12:05", strings.TrimSpace(lines[1]), "the time right under the logo")
	require.Contains(t, lines[2], "1 ready")

	m = saverAt(t, 80, 24, "screensaver_clock", "12h", "reduce_motion", true)
	lines, _ = saverLines(m)
	require.Equal(t, "12:05 PM", strings.TrimSpace(lines[1]))

	m = saverAt(t, 80, 24)
	require.NotContains(t, ansi.Strip(m.View()), "12:05", "off by default")
}

// Short terminals drop the clock before the logo and the counts.
func TestScreensaverClockGivesWayWhenShort(t *testing.T) {
	m := saverAt(t, 80, 2, "screensaver_clock", "24h")
	text := ansi.Strip(m.View())
	require.Contains(t, text, saverLogo)
	require.Contains(t, text, "1 ready")
	require.NotContains(t, text, "12:05")
}

// With the clock on, the timer wakes on the minute so the time is never
// a minute behind.
func TestIdleDelayClock(t *testing.T) {
	m, now, _ := saverModel(t)
	start := *now
	m.saverOn, m.saverSince = true, start.Add(-10*time.Second)
	require.NoError(t, m.Store.Set("reduce_motion", true))
	require.Equal(t, idleMaxWait, idleDelay(m, start.Add(20*time.Second)), "no clock: no minute ticks")
	require.NoError(t, m.Store.Set("screensaver_clock", "24h"))
	require.Equal(t, 40*time.Second, idleDelay(m, start.Add(20*time.Second)))

	require.NoError(t, m.Store.Set("reduce_motion", false))
	require.Equal(t, 10*time.Second, idleDelay(m, start.Add(40*time.Second)), "the drift step (50s shown) comes before the minute")
	require.Equal(t, 5*time.Second, idleDelay(m, start.Add(55*time.Second)), "the minute comes before the next drift step")
}

func TestScreensaverDriftSetting(t *testing.T) {
	m := saverAt(t, 80, 24, "screensaver_drift", false)
	shown := m.saverSince
	row0, col0 := saverOrigin(t, m)
	clock := shown
	m.Clock = func() time.Time { return clock }
	for minute := 1; minute <= 30; minute++ {
		clock = shown.Add(time.Duration(minute) * time.Minute)
		r, c := saverOrigin(t, m)
		require.Equal(t, [2]int{row0, col0}, [2]int{r, c}, "minute %d", minute)
	}
	m.saverOn = true
	require.Equal(t, idleMaxWait, idleDelay(m, shown.Add(80*time.Second)), "no drift steps to wait for")
}

func TestScreensaverConfigSettingsPage(t *testing.T) {
	m := Model{
		Store:     settings.NewStore(),
		StorePath: filepath.Join(t.TempDir(), "config.yaml"),
		Settings:  &SettingsState{CursorID: "screensaver_counts"},
	}
	m = applySettingsAction(m, +1)
	require.Equal(t, "vertical", m.Store.Get("screensaver_counts"))
	m.Settings.CursorID = "screensaver_clock"
	m = applySettingsAction(m, +1)
	require.Equal(t, "24h", m.Store.Get("screensaver_clock"))
	m.Settings.CursorID = "screensaver_drift"
	m = applySettingsAction(m, 0)
	require.Equal(t, false, m.Store.Get("screensaver_drift"))

	m.Height = 80
	page := ansi.Strip(renderSettings(m, 100))
	for _, label := range []string{"Screensaver counts", "Screensaver clock", "Screensaver drift"} {
		require.Contains(t, page, label)
	}
}
