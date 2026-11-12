package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/testutil/golden"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// goldenNow pins every relative time on a golden screen.
var goldenNow = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

type goldenSize struct{ w, h int }

var goldenSizes = []goldenSize{{80, 24}, {120, 40}}

// goldenBoard is a board on a fixed clock with a session in each state, the
// spinner on frame 0 and no blink.
func goldenBoard(t *testing.T, sz goldenSize, detailed bool) Model {
	t.Helper()
	t.Setenv("HOME", "/home/dev")
	exit := 1
	sessions := append(layoutSessions(goldenNow), protocol.SessionSummary{
		ID: "f", ShortID: "ff01", ToolID: "claude", ModelID: "opus", Slug: "failing-job",
		State: protocol.StateFailed, ExitCode: &exit, LastLine: "error: migration 0042 timed out",
	})
	m := layoutModel(goldenNow, sz.w, sessions...)
	m.Height = sz.h
	m.Detailed = detailed
	m.SpinnerTick = 0
	return m
}

// goldenLoops is the golden board with two sessions owned by loops (one
// running, one paused for you) and the loops feature on.
func goldenLoops(t *testing.T, sz goldenSize, focus Focus) Model {
	t.Helper()
	m := goldenBoard(t, sz, false)
	if err := m.Store.Set("loops", true); err != nil {
		t.Fatal(err)
	}
	m.Loops = testLoops()
	m.Sessions[2].LoopID, m.Sessions[2].LoopIter = loopA, 3 // working
	m.Sessions[0].LoopID, m.Sessions[0].LoopIter = loopB, 2 // needs you
	m.Loops[0].SessionID, m.Loops[1].SessionID = m.Sessions[2].ID, m.Sessions[0].ID
	m.Focus = focus
	return m
}

// goldenDoneFold is the golden board with a Done column long enough to fold:
// its keys sit at the end of the help line.
func goldenDoneFold(t *testing.T, sz goldenSize) Model {
	t.Helper()
	m := goldenBoard(t, sz, false)
	for i := range 3 {
		m.Sessions = append(m.Sessions, protocol.SessionSummary{
			ID: fmt.Sprintf("d%d", i), ShortID: fmt.Sprintf("dd0%d", i), ToolID: "claude",
			Slug: fmt.Sprintf("shipped-%d", i), State: protocol.StateDone, LastEventAt: goldenNow.Add(-time.Hour),
		})
	}
	return m
}

type goldenScreen struct {
	name   string
	styled bool // also pin the TrueColor SGR output
	model  func(t *testing.T, sz goldenSize) Model
}

// sizesOf is the sizes a screen is pinned at.
func (sc goldenScreen) sizesOf() []goldenSize {
	if sz, ok := goldenScreenSizes[sc.name]; ok {
		return sz
	}
	return goldenSizes
}

// goldenScreenSizes overrides the sizes of the screens that are pinned at
// others than goldenSizes.
var goldenScreenSizes = map[string][]goldenSize{
	"usage":           {{80, 24}, {120, 40}, goldenMobile},
	"header_meter":    {{60, 24}, {80, 24}, {120, 40}},
	"undo_confirm":    {{80, 24}, {120, 40}, goldenMobile},
	"undo_shared":     {{80, 24}, {120, 40}, goldenMobile},
	"undo_forced":     {{80, 24}, {120, 40}, goldenMobile},
	"undo_done":       {{80, 24}, {120, 40}, goldenMobile},
	"board_done_fold": {{60, 24}, {80, 24}, {120, 40}, goldenMobile},
}

// goldenMobile is a terminal narrow enough for the mobile layout.
var goldenMobile = goldenSize{50, 30}

func goldenScreens() []goldenScreen {
	return []goldenScreen{
		{"board_simple", true, func(t *testing.T, sz goldenSize) Model { return goldenBoard(t, sz, false) }},
		{"board_detailed", false, func(t *testing.T, sz goldenSize) Model { return goldenBoard(t, sz, true) }},
		{"board_done_fold", false, goldenDoneFold},
		{"board_hints_off", false, func(t *testing.T, sz goldenSize) Model {
			m := goldenDoneFold(t, sz)
			if err := m.Store.Set("show_help_bar", false); err != nil {
				t.Fatal(err)
			}
			return m
		}},
		{"help", false, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m.Focus = FocusHelp
			return m
		}},
		{"settings", true, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m.Focus = FocusSettings
			m.Settings = &SettingsState{CursorID: settings.Registry[0].ID}
			return m
		}},
		{"help_filtered", false, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m.Focus = FocusHelp
			m.Help = &HelpState{Filter: "undo"}
			return m
		}},
		{"settings_lua", true, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m.Focus = FocusSettings
			m.Store, m.StorePath = luaLocked(t, []string{"spinner", "theme"}, "moon", "default")
			m.Settings = &SettingsState{CursorID: "spinner", Search: "spin"}
			return m
		}},
		{"settings_search", true, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m.Focus = FocusSettings
			m.Settings = &SettingsState{CursorID: "spinner", Search: "spin"}
			if err := m.Store.Set("spinner", "moon"); err != nil {
				t.Fatal(err)
			}
			return m
		}},
		{"wizard", false, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m.Focus = FocusWizard
			m.Wizard = &WizardState{Step: wizProvider, Tools: demoTools()}
			return m
		}},
		{"stats", false, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m.Focus = FocusStats
			return m
		}},
		{"fail", false, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			failed := []protocol.SessionSummary{m.Sessions[len(m.Sessions)-1], m.Sessions[5]}
			failed[0].LastEventAt = time.Time{}
			m.Focus = FocusFail
			m.Fail = &FailState{Sessions: failed, LogLines: []string{"daemon: session ff01 exited 1"}}
			return m
		}},
		{"overview", false, func(t *testing.T, sz goldenSize) Model {
			m := overviewBoard(t, true)
			m.Width, m.Height = sz.w, sz.h
			m, _ = executeCommand(m, "overview")
			return m
		}},
		{"usage", true, usageModelAt},
		{"header_meter", true, meterModel},
		{"undo_confirm", false, func(t *testing.T, sz goldenSize) Model { return undoDialogModel(t, sz, "confirm") }},
		{"undo_shared", false, func(t *testing.T, sz goldenSize) Model { return undoDialogModel(t, sz, "shared") }},
		{"undo_forced", false, func(t *testing.T, sz goldenSize) Model { return undoDialogModel(t, sz, "forced") }},
		{"undo_done", false, func(t *testing.T, sz goldenSize) Model { return undoDialogModel(t, sz, "done") }},
		{"wizard_models", false, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m.DaemonTools, m.toolsKnown = daemonTools(t, "claude", "qwen"), true
			m, _ = executeCommand(m, "new")
			m = sendKey(m, key("j"))
			return sendKey(m, key("enter"))
		}},
		{"settings_onboarding", true, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m.DaemonTools, m.toolsKnown = daemonTools(t, "claude", "qwen"), true
			m, _ = openSettings(m)
			m.Settings.CursorID = onboardPrefix + "codex"
			return m
		}},
		{"board_frame", true, func(t *testing.T, sz goldenSize) Model {
			return framedGolden(t, goldenBoard(t, sz, false), sz)
		}},
		{"workspace_board", true, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			if err := m.Store.Set("workspaces", true); err != nil {
				t.Fatal(err)
			}
			// Half the sessions belong to the workspace; the rest are elsewhere.
			for i := range m.Sessions {
				if i%2 == 0 {
					m.Sessions[i].WorkspaceID = wsA
				} else {
					m.Sessions[i].WorkspaceID = wsB
				}
			}
			m.WS = wsScopeA()
			m.wsTagging = true
			m = reselectOutOfScope(m)
			m.TermW, m.TermH = sz.w, sz.h // the border is on by default here
			return syncLayout(m)
		}},
		{"loop_rows", true, func(t *testing.T, sz goldenSize) Model {
			return goldenLoops(t, sz, FocusBoard)
		}},
		{"loops_overlay", false, func(t *testing.T, sz goldenSize) Model {
			return goldenLoops(t, sz, FocusLoops)
		}},
		{"loop_stop_confirm", false, func(t *testing.T, sz goldenSize) Model {
			m := goldenLoops(t, sz, FocusConfirmLoopStop)
			m.PendingLoopStop = loopA
			return m
		}},
		{"attach_frame", false, func(t *testing.T, sz goldenSize) Model {
			m := bannerModel(t, sz.w, sz.h, bannerSession())
			return framedGolden(t, m, sz)
		}},
		{"attach", false, func(t *testing.T, sz goldenSize) Model {
			return bannerModel(t, sz.w, sz.h, bannerSession())
		}},
	}
}

// sizeGolden gives a model the golden size; one that already has a terminal
// size (the framed screens) keeps its layout size.
func sizeGolden(m *Model, sz goldenSize) {
	if m.TermW == 0 {
		m.Width, m.Height = sz.w, sz.h
	}
}

// framedGolden turns window_border on in a fixed color and lays m out in the
// terminal of size sz.
func framedGolden(t *testing.T, m Model, sz goldenSize) Model {
	t.Helper()
	if err := m.Store.Set("window_border", true); err != nil {
		t.Fatal(err)
	}
	m.borderRunColor = "tangerine"
	m.TermW, m.TermH = sz.w, sz.h
	if m.Attach != nil {
		m.Attach.resize = func(uint16, uint16) error { return nil }
	}
	return syncLayout(m)
}

// withProfile runs f under a color profile and restores the previous one.
func withProfile(p termenv.Profile, f func()) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(p)
	defer lipgloss.SetColorProfile(prev)
	f()
}

func TestGoldenScreens(t *testing.T) {
	for _, sc := range goldenScreens() {
		t.Run(sc.name, func(t *testing.T) {
			var sections []golden.Section
			for _, sz := range sc.sizesOf() {
				m := sc.model(t, sz)
				sizeGolden(&m, sz)
				withProfile(termenv.Ascii, func() {
					sections = append(sections, golden.Section{
						Name: fmt.Sprintf("plain.%dx%d", sz.w, sz.h),
						Body: ansi.Strip(m.View()),
					})
				})
			}
			if sc.styled {
				for _, sz := range sc.sizesOf() {
					m := sc.model(t, sz)
					sizeGolden(&m, sz)
					withProfile(termenv.TrueColor, func() {
						sections = append(sections, golden.Section{
							Name: fmt.Sprintf("styled.%dx%d", sz.w, sz.h),
							Body: m.View(),
						})
					})
				}
			}
			golden.Check(t, "testdata/golden/"+sc.name+".txtar", sections...)
		})
	}
}

// Every golden screen is exactly the terminal size, so a restyle that adds or
// drops a row shows up as a size failure, not only as a diff.
func TestGoldenScreensFillTerminal(t *testing.T) {
	for _, sc := range goldenScreens() {
		for _, sz := range sc.sizesOf() {
			m := sc.model(t, sz)
			sizeGolden(&m, sz)
			lines := strings.Split(ansi.Strip(m.View()), "\n")
			if len(lines) != sz.h {
				t.Errorf("%s %dx%d: %d lines", sc.name, sz.w, sz.h, len(lines))
			}
		}
	}
}
