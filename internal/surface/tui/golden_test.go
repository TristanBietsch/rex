package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/features/spawnctx"
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

// usd is a cost_usd value.
func usd(v float64) *float64 { return &v }

// goldenIconSessions is the row-icon board of spec §11: a question and a review in
// Ready, a shell and a subagent wait, a loop, spend on the Claude rows only,
// long runs, and finished rows.
func goldenIconSessions(now time.Time) []protocol.SessionSummary {
	exit := 1
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	resume := func(n int) string { return fmt.Sprintf("%08x-0000-4000-8000-000000000000", n) }
	return []protocol.SessionSummary{
		{ID: "i1", ShortID: "7d4f", ToolID: "claude", ModelID: "opus", Effort: "high", Tokens: 12345, CostUSD: usd(0.31),
			Slug: "dark-mode", Title: "system theme or an explicit toggle?", LastLine: "Should the toggle follow the system theme?",
			State: protocol.StateNeedsInput, WaitReason: protocol.WaitInput, ResumeID: resume(0x1a2b3c4d),
			StartedAt: ago(time.Hour), LastEventAt: ago(4 * time.Minute)},
		{ID: "i2", ShortID: "b203", ToolID: "claude", ModelID: "opus", Effort: "max", Tokens: 123456, CostUSD: usd(2.10),
			Slug: "release-notes", Title: "draft the 0.9 notes", State: protocol.StateNeedsInput, WaitReason: protocol.WaitReview,
			ResumeID: resume(0x5e6f7a8b), StartedAt: ago(2*time.Hour + 20*time.Minute), LastEventAt: ago(11 * time.Minute)},
		{ID: "i3", ShortID: "3c8a", ToolID: "claude", ModelID: "sonnet", Effort: "medium", Tokens: 48000, CostUSD: usd(0.84),
			Slug: "invoice-rounding-tests", Title: "run the rounding suite", State: protocol.StateWorking,
			WaitingOn: protocol.WaitShell, WaitingSince: ago(42 * time.Second), WorkingSince: ago(12 * time.Minute),
			ResumeID: resume(0x9c0d1e2f), StartedAt: ago(29 * time.Minute), LastEventAt: ago(40 * time.Second)},
		{ID: "i4", ShortID: "9e51", ToolID: "claude", ModelID: "opus", Effort: "high", Tokens: 77000, CostUSD: usd(1.07),
			Slug: "api-research", Title: "compare the rate-limit headers", State: protocol.StateWorking, Subagents: 2,
			WaitingOn: protocol.WaitSubagent, WaitingSince: ago(3 * time.Minute), WorkingSince: ago(5 * time.Minute),
			ResumeID: resume(0x3a4b5c6d), StartedAt: ago(12 * time.Minute), LastEventAt: ago(2 * time.Minute)},
		{ID: "i5", ShortID: "c4d1", ToolID: "claude", ModelID: "opus", Effort: "max", Tokens: 310000, CostUSD: usd(4.92),
			Slug: "payment-migration-v2", Title: "port billing to the new processor", State: protocol.StateWorking, Subagents: 3,
			Description:  "porting billing to the new processor: 12 of 14 handlers moved, webhooks next",
			WorkingSince: ago(45 * time.Minute), ResumeID: resume(0x7e8f9a0b), StartedAt: ago(time.Hour + 2*time.Minute),
			LastEventAt: ago(time.Minute), LoopID: loopA, LoopIter: 3},
		{ID: "i6", ShortID: "e0f2", ToolID: "codex", ModelID: "gpt-5-codex", Effort: "medium", Tokens: 22000,
			Slug: "docs-sweep", Title: "sweep the docs for dead links", State: protocol.StateWorking,
			WorkingSince: ago(5 * time.Minute), StartedAt: ago(5 * time.Minute), LastEventAt: ago(7 * time.Second)},
		{ID: "i7", ShortID: "a913", ToolID: "ollama", ModelID: "qwen3-coder", Tokens: 900,
			Slug: "i18n-strings-extraction-pass", Title: "extract the UI strings", State: protocol.StateWorking,
			WorkingSince: ago(2*time.Hour + 9*time.Minute), StartedAt: ago(2*time.Hour + 9*time.Minute), LastEventAt: ago(3 * time.Minute)},
		{ID: "i8", ShortID: "2b7e", ToolID: "claude", ModelID: "haiku", Tokens: 88000, CostUSD: usd(3.40),
			Slug: "test-coverage", Title: "raise billing coverage", State: protocol.StateDone, ResumeID: resume(0xaa11bb22),
			StartedAt: ago(3 * time.Hour), EndedAt: ago(10 * time.Minute), LastEventAt: ago(9 * time.Minute)},
		{ID: "i9", ShortID: "ff01", ToolID: "claude", ModelID: "opus", Slug: "failing-job", Title: "run the migration",
			State: protocol.StateFailed, ExitCode: &exit, LastLine: "error: migration 0042 timed out",
			StartedAt: ago(20 * time.Minute), EndedAt: ago(8 * time.Minute)},
	}
}

// goldenIcons is the row-icon board on the golden clock: row_icons on,
// usage on, the loop running, three rows unseen (pinned EnteredAt after
// SeenAt) and one seen. Compact rows at 24 lines so the board fits.
func goldenIcons(t *testing.T, sz goldenSize, detailed bool, kv ...any) Model {
	t.Helper()
	t.Setenv("HOME", "/home/dev")
	m := layoutModel(goldenNow, sz.w, goldenIconSessions(goldenNow)...)
	m.Height, m.Detailed, m.SpinnerTick, m.SelectedID = sz.h, detailed, 0, "i5"
	set := append([]any{"row_icons", true, "usage", true}, kv...)
	if sz.h <= 24 {
		set = append(set, "row_density", "compact")
	}
	for i := 0; i+1 < len(set); i += 2 {
		if err := m.Store.Set(set[i].(string), set[i+1]); err != nil {
			t.Fatal(err)
		}
	}
	m.Loops = testLoops()[:1]
	m.Loops[0].SessionID = "i5"
	seen, entered := goldenNow.Add(-time.Hour), goldenNow.Add(-2*time.Minute)
	m.SeenAt = map[string]time.Time{"i1": seen, "i2": goldenNow, "i8": seen, "i9": seen}
	m.EnteredAt = map[string]time.Time{"i1": entered, "i2": entered, "i8": entered, "i9": entered}
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

// defaultRowIcons puts row_icons back to its default for a full settings
// page, which lists it (and counts changed settings) but shows no board.
func defaultRowIcons(t *testing.T, m Model) Model {
	t.Helper()
	if err := m.Store.Reset("row_icons"); err != nil {
		t.Fatal(err)
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
	"usage":                  {{80, 24}, {120, 40}, goldenMobile},
	"header_meter":           {{60, 24}, {80, 24}, {120, 40}},
	"undo_confirm":           {{80, 24}, {120, 40}, goldenMobile},
	"undo_shared":            {{80, 24}, {120, 40}, goldenMobile},
	"undo_forced":            {{80, 24}, {120, 40}, goldenMobile},
	"undo_done":              {{80, 24}, {120, 40}, goldenMobile},
	"board_done_fold":        {{60, 24}, {80, 24}, {120, 40}, goldenMobile},
	"settings_page":          {{120, 40}, {80, 24}, {50, 20}},
	"help_page":              {{120, 40}, {80, 24}, {50, 20}},
	"help_page_narrow":       {{50, 20}},
	"settings_page_frame":    {{80, 24}},
	"settings_page_narrow":   {{50, 20}},
	"board_icons_detailed":   {{80, 24}, {120, 40}, {150, 40}},
	"newagent_empty":         {{120, 40}, {80, 24}, {50, 20}},
	"newagent_agent":         {{120, 40}, {80, 24}, {50, 20}},
	"newagent_models_filter": {{120, 40}, {80, 24}, {50, 20}},
	"newagent_filled":        {{120, 40}, {80, 24}, {50, 20}},
	"newagent_preview":       {{120, 40}, {80, 24}, {50, 20}},
	"newagent_import":        {{120, 40}, {80, 24}, {50, 20}},
	"newagent_loading":       {{120, 40}, {80, 24}, {50, 20}},
	"newagent_errors":        {{120, 40}, {80, 24}, {50, 20}},
	"newagent_frame":         {{80, 24}},
	"newagent_ansi":          {{80, 24}, {50, 20}},
}

// goldenMobile is a terminal narrow enough for the mobile layout.
var goldenMobile = goldenSize{50, 30}

func goldenScreens() []goldenScreen {
	return []goldenScreen{
		{"board_simple", true, func(t *testing.T, sz goldenSize) Model { return goldenBoard(t, sz, false) }},
		{"board_detailed", false, func(t *testing.T, sz goldenSize) Model { return goldenBoard(t, sz, true) }},
		{"board_icons_simple", true, func(t *testing.T, sz goldenSize) Model { return goldenIcons(t, sz, false) }},
		{"board_icons_detailed", false, func(t *testing.T, sz goldenSize) Model { return goldenIcons(t, sz, true) }},
		{"board_icons_ascii", false, func(t *testing.T, sz goldenSize) Model {
			return goldenIcons(t, sz, false, "ascii_glyphs", true, "loops", true)
		}},
		{"board_icons_loops", true, func(t *testing.T, sz goldenSize) Model {
			return goldenIcons(t, sz, false, "loops", true)
		}},
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
			m = overlayMode(t, m)
			m.Focus = FocusHelp
			return m
		}},
		{"settings", true, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m = overlayMode(t, m)
			m.Focus = FocusSettings
			m.Settings = &SettingsState{CursorID: settings.Registry[0].ID}
			return m
		}},
		{"help_filtered", false, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m = overlayMode(t, m)
			m.Focus = FocusHelp
			m.Help = &HelpState{Filter: "undo"}
			return m
		}},
		{"settings_lua", true, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m.Focus = FocusSettings
			m.Store, m.StorePath = luaLocked(t, []string{"spinner", "theme"}, "moon", "default")
			if err := m.Store.Set("row_icons", false); err != nil { // the board behind pins the text column
				t.Fatal(err)
			}
			m = overlayMode(t, m)
			m.Settings = &SettingsState{CursorID: "spinner", Search: "spin"}
			return m
		}},
		{"settings_page_lua", true, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m.Focus = FocusSettings
			m.Store, m.StorePath = luaLocked(t, []string{"spinner", "theme"}, "moon", "default")
			m.Settings = &SettingsState{CursorID: "spinner"}
			return m
		}},
		{"settings_search", true, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m = overlayMode(t, m)
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
			m = wizardFlag(t, m)
			m.DaemonTools, m.toolsKnown = daemonTools(t, "claude", "qwen"), true
			m, _ = executeCommand(m, "new")
			m = sendKey(m, key("j"))
			return sendKey(m, key("enter"))
		}},
		{"wizard_all_agents", false, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m = wizardFlag(t, m)
			m.DaemonTools, m.toolsKnown = daemonTools(t, "claude", "codex", "antigravity", "ollama", "qwen", "cursor", "grok"), true
			m, _ = executeCommand(m, "new")
			return m
		}},
		{"settings_onboarding", true, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m = overlayMode(t, m)
			m.DaemonTools, m.toolsKnown = daemonTools(t, "claude", "qwen"), true
			m, _ = openSettings(m)
			m.Settings.CursorID = onboardPrefix + "codex"
			return m
		}},
		{"settings_page", true, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m.Focus = FocusSettings
			m = defaultRowIcons(t, m)
			m.Settings = &SettingsState{CursorID: settings.Registry[0].ID}
			return m
		}},
		{"settings_page_search", true, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m.Focus = FocusSettings
			m = defaultRowIcons(t, m)
			if err := m.Store.Set("spinner", "moon"); err != nil {
				t.Fatal(err)
			}
			m.Settings = &SettingsState{CursorID: "spinner", Search: "spin"}
			return m
		}},
		{"settings_page_narrow", true, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m.Focus = FocusSettings
			// A section-pane focus left from a wider page draws as rows.
			m.Settings = &SettingsState{CursorID: "mouse_enabled", Pane: paneSections}
			return m
		}},
		{"help_page", false, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m.Focus = FocusHelp
			return m
		}},
		{"help_page_filtered", true, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m.Focus = FocusHelp
			m.Help = &HelpState{Filter: "undo"}
			return m
		}},
		{"help_page_narrow", true, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m.Focus = FocusHelp
			// Session commands, scrolled, with a group-pane focus left from
			// a wider page (drawn as rows).
			m.Help = &HelpState{Group: 6, Scroll: 2, Pane: paneSections}
			return m
		}},
		{"settings_page_frame", true, func(t *testing.T, sz goldenSize) Model {
			m := goldenBoard(t, sz, false)
			m.Focus = FocusSettings
			m = defaultRowIcons(t, m)
			m.Settings = &SettingsState{CursorID: settings.Registry[0].ID}
			return framedGolden(t, m, sz)
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
		{"newagent_empty", true, func(t *testing.T, sz goldenSize) Model {
			m, _ := goldenNewAgentBoard(t, sz)
			m.DaemonTools, m.toolsKnown = daemonTools(t, "claude", "codex"), true
			m, _ = executeCommand(m, "new")
			m.NewAgent.Indexing = false // indexed: mockup A ("none yet"), not mockup F's loading row
			return m
		}},
		{"newagent_agent", true, func(t *testing.T, sz goldenSize) Model {
			m, home := goldenNewAgentBoard(t, sz)
			writeHomeSkill(t, home, "go-testing", "table-driven tests and goldens")
			writeHomeSkill(t, home, "release", "cut a release branch")
			m.DaemonTools, m.toolsKnown = daemonTools(t, "claude", "codex", "ollama", "cursor", "grok"), true
			m, _ = executeCommand(m, "new")
			m = sendKey(m, tea.KeyMsg{Type: tea.KeyCtrlG})
			m.NewAgent.SkillSel = map[string]bool{"go-testing": true}
			return m
		}},
		{"newagent_models_filter", false, func(t *testing.T, sz goldenSize) Model {
			m, _ := goldenNewAgentBoard(t, sz)
			m.DaemonTools, m.toolsKnown = cursorModels(t), true
			m, _ = executeCommand(m, "new")
			m = sendKey(m, tea.KeyMsg{Type: tea.KeyCtrlG})
			m = sendKey(m, key("j")) // Cursor CLI
			m = sendKey(m, key("/"))
			return typeText(m, "son")
		}},
		{"newagent_filled", true, goldenNewAgentFilled},
		{"newagent_preview", false, func(t *testing.T, sz goldenSize) Model {
			m := goldenNewAgentFilled(t, sz)
			return sendKey(m, key("enter"))
		}},
		{"newagent_import", false, goldenNewAgentImport},
		{"newagent_errors", true, func(t *testing.T, sz goldenSize) Model {
			// Mockup F: the workspace still indexing under CONTEXT, and a
			// launch the daemon refused, with the draft kept.
			m, _ := goldenNewAgentBoard(t, sz)
			m.DaemonTools, m.toolsKnown = daemonTools(t, "claude", "codex"), true
			m, _ = executeCommand(m, "new") // the walk never comes back
			m.NewAgent.indexCount.Store(3812)
			m = typeText(m, "fix the flaky auth test")
			m.NewAgent.Launching, m.NewAgent.launchGen = true, -1
			m, _ = handleNewAgentLaunched(m, newAgentLaunchedMsg{Err: errors.New("daemon is not running (rex daemon start)"), gen: -1})
			return m
		}},
		{"newagent_loading", false, func(t *testing.T, sz goldenSize) Model {
			m, _ := goldenNewAgentBoard(t, sz)
			m.DaemonTools, m.toolsKnown = daemonTools(t, "claude", "codex"), true
			m, _ = executeCommand(m, "new") // the walk never comes back
			m = sendKey(m, tea.KeyMsg{Type: tea.KeyCtrlO})
			m.NewAgent.indexCount.Store(3812)
			return m
		}},
		{"newagent_frame", true, func(t *testing.T, sz goldenSize) Model {
			return framedGolden(t, goldenNewAgentFilled(t, sz), sz)
		}},
		{"newagent_ansi", true, func(t *testing.T, sz goldenSize) Model {
			// Last: the theme is global until the test ends.
			m := goldenNewAgentFilled(t, sz)
			if err := m.Store.Set("theme", "ansi"); err != nil {
				t.Fatal(err)
			}
			applyThemeNamed(&m, "ansi")
			t.Cleanup(func() { applyTheme("default") })
			return m
		}},
	}
}

// goldenNewAgentImport is mockup D: the picker over a small checkout,
// "auth" typed, one row marked and a denylisted key file in the list, with
// one item already in the context.
func goldenNewAgentImport(t *testing.T, sz goldenSize) Model {
	t.Helper()
	m, home := goldenNewAgentBoard(t, sz)
	repo := filepath.Join(home, "dev", "rex")
	for name, size := range map[string]int{
		"internal/auth/auth_test.go":        4300,
		"internal/auth/testdata/login.json": 210,
		"internal/auth/testdata/server.pem": 1700,
		"cmd/rex/auth_test_helpers.go":      1126,
		"README.md":                         2400,
	} {
		p := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(strings.Repeat("x", size)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", "") // the plain walk: the checkout is not a real repo
	m.DaemonTools, m.toolsKnown = daemonTools(t, "claude", "codex"), true
	m, cmd := executeCommand(m, "new")
	next, _ := m.Update(indexMsg(t, cmd))
	m = next.(Model)
	if err := addPath(m.NewAgent, "README.md", false); err != nil {
		t.Fatal(err)
	}
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyCtrlO})
	m = typeText(m, "auth")
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyDown})
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyDown})
	m = sendKey(m, key(" "))
	return sendKey(m, tea.KeyMsg{Type: tea.KeyUp})
}

// goldenNewAgentBoard is the golden board in a temp home with a checkout at
// ~/dev/rex as the board's workspace (not a chdir: the golden file is
// written relative to the package): the page names it, and the trust
// section offers it. It returns the home too.
func goldenNewAgentBoard(t *testing.T, sz goldenSize) (Model, string) {
	t.Helper()
	m := goldenBoard(t, sz, false)
	home := t.TempDir()
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	repo := filepath.Join(home, "dev", "rex")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := m.Store.Set("workspaces", true); err != nil {
		t.Fatal(err)
	}
	m.WS = &WorkspaceScope{ID: wsA, Name: "rex", Root: repo}
	return m, home
}

// goldenNewAgentFilled is mockup B: a task, a file, a folder and a 38-line
// paste (the file and folder set directly until the import picker), the
// folder trusted for the session, the paste under the context cursor.
func goldenNewAgentFilled(t *testing.T, sz goldenSize) Model {
	t.Helper()
	m, _ := goldenNewAgentBoard(t, sz)
	m.DaemonTools, m.toolsKnown = daemonTools(t, "claude", "codex"), true
	m, _ = executeCommand(m, "new")
	m = typeText(m, "the auth test is flaky on CI, see the paste. fix it and add a regression test. ")
	m.NewAgent.Items = []spawnctx.Item{
		{Kind: spawnctx.File, Name: "internal/auth/auth_test.go", Bytes: 4300},
		{Kind: spawnctx.Dir, Name: "internal/auth/testdata", Files: 12, Bytes: 1100},
	}
	log := "--- FAIL: TestLogin (0.31s)\n" +
		"    auth_test.go:88: token expired: got 401, want 200\n" +
		"FAIL\n" +
		"FAIL  example.com/app/internal/auth 1.204s\n"
	for i := range 34 {
		log += fmt.Sprintf("    trace %02d: example.com/app/internal/auth.check\n", i+1)
	}
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(log), Paste: true})
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyCtrlT})
	m = sendKey(m, tea.KeyMsg{Type: tea.KeyTab}) // Context
	m = sendKey(m, key("j"))
	return sendKey(m, key("j"))
}

// wizardFlag turns new_agent_page off, so n and :new open the popup wizard.
func wizardFlag(t *testing.T, m Model) Model {
	t.Helper()
	if m.Store == nil {
		m.Store = settings.NewStore()
	}
	if err := m.Store.Set("new_agent_page", false); err != nil {
		t.Fatal(err)
	}
	return m
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
