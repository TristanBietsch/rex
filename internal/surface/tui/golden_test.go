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

type goldenScreen struct {
	name   string
	styled bool // also pin the TrueColor SGR output
	model  func(t *testing.T, sz goldenSize) Model
}

func goldenScreens() []goldenScreen {
	return []goldenScreen{
		{"board_simple", true, func(t *testing.T, sz goldenSize) Model { return goldenBoard(t, sz, false) }},
		{"board_detailed", false, func(t *testing.T, sz goldenSize) Model { return goldenBoard(t, sz, true) }},
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
		{"board_frame", true, func(t *testing.T, sz goldenSize) Model {
			return framedGolden(t, goldenBoard(t, sz, false), sz)
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
			for _, sz := range goldenSizes {
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
				for _, sz := range goldenSizes {
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
		for _, sz := range goldenSizes {
			m := sc.model(t, sz)
			sizeGolden(&m, sz)
			lines := strings.Split(ansi.Strip(m.View()), "\n")
			if len(lines) != sz.h {
				t.Errorf("%s %dx%d: %d lines", sc.name, sz.w, sz.h, len(lines))
			}
		}
	}
}
