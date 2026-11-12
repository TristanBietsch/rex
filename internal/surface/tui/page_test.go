package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/version"
)

// overlayMode turns fullscreen_pages off: settings and help draw as the
// floating overlays, for the tests that pin overlay geometry.
func overlayMode(t *testing.T, m Model) Model {
	t.Helper()
	if m.Store == nil {
		m.Store = settings.NewStore()
	}
	if err := m.Store.Set("fullscreen_pages", false); err != nil {
		t.Fatal(err)
	}
	return m
}

// pageModel is the layout board at w x h with settings or help open.
func pageModel(t *testing.T, w, h int, focus Focus) Model {
	t.Helper()
	m := layoutModel(goldenNow, w, layoutSessions(goldenNow)...)
	m.Height = h
	m.Detailed = false
	return openPage(t, m, focus)
}

func openPage(t *testing.T, m Model, focus Focus) Model {
	t.Helper()
	switch focus {
	case FocusSettings:
		m, _ = openSettings(m)
	case FocusHelp:
		m = openHelp(m)
	default:
		t.Fatalf("not a page: %v", focus)
	}
	if m.Focus != focus {
		t.Fatalf("focus = %v, want %v", m.Focus, focus)
	}
	return m
}

func TestPagesFlagDefaultOn(t *testing.T) {
	s, ok := settings.Find("fullscreen_pages")
	if !ok {
		t.Fatal("fullscreen_pages is not in the registry")
	}
	if s.Default != true || s.Section != settings.SectionFeatures {
		t.Fatalf("fullscreen_pages: default %v section %s, want true Features", s.Default, s.Section)
	}
	if !pagesOn(Model{}) {
		t.Fatal("pagesOn(Model{}) = false, want true")
	}
}

func TestPageFrameRows(t *testing.T) {
	for _, sz := range []goldenSize{{120, 40}, {80, 24}, {50, 20}, {30, 10}} {
		board := layoutModel(goldenNow, sz.w, layoutSessions(goldenNow)...)
		board.Height = sz.h
		board.Detailed = false
		logoCol := strings.Index(viewLines(board)[1], "∴ REX")
		if logoCol < 0 {
			t.Fatalf("%dx%d: board header has no logo", sz.w, sz.h)
		}
		for _, pg := range []struct {
			focus Focus
			title string
		}{{FocusSettings, "∴ REX › SETTINGS"}, {FocusHelp, "∴ REX › HELP"}} {
			m := pageModel(t, sz.w, sz.h, pg.focus)
			lines := viewLines(m)
			name := pg.title
			if len(lines) != sz.h {
				t.Fatalf("%s %dx%d: %d lines", name, sz.w, sz.h, len(lines))
			}
			for i, l := range lines {
				if ansi.StringWidth(l) > sz.w {
					t.Errorf("%s %dx%d line %d: width %d", name, sz.w, sz.h, i, ansi.StringWidth(l))
				}
			}
			if strings.TrimSpace(lines[0]) != "" {
				t.Errorf("%s %dx%d: line 0 %q, want blank", name, sz.w, sz.h, lines[0])
			}
			if got := strings.Index(lines[1], pg.title); got != logoCol {
				t.Errorf("%s %dx%d: title at %d in %q, want %d", name, sz.w, sz.h, got, lines[1], logoCol)
			}
			for _, i := range []int{2, sz.h - 2} {
				if strings.Trim(lines[i], "─") != "" || lines[i] == "" {
					t.Errorf("%s %dx%d line %d: %q, want a full divider", name, sz.w, sz.h, i, lines[i])
				}
			}
			if !strings.Contains(lines[sz.h-1], "esc") {
				t.Errorf("%s %dx%d: last line %q has no esc", name, sz.w, sz.h, lines[sz.h-1])
			}
		}
	}
}

// With show_help_bar off the page keeps its hint row but blanks it, like the
// board does; a modal prompt (search) still shows its keys.
func TestPageHintsFollowHelpBar(t *testing.T) {
	for _, focus := range []Focus{FocusSettings, FocusHelp} {
		m := pageModel(t, 80, 24, focus)
		if err := m.Store.Set("show_help_bar", false); err != nil {
			t.Fatal(err)
		}
		lines := viewLines(m)
		if len(lines) != 24 {
			t.Fatalf("focus %v: %d lines, want 24", focus, len(lines))
		}
		if strings.Contains(lines[23], "esc") {
			t.Errorf("focus %v: help bar off, last line %q still has hints", focus, lines[23])
		}
		m = sendKey(m, key("/"))
		lines = viewLines(m)
		if len(lines) != 24 || !strings.Contains(lines[23], "esc") {
			t.Errorf("focus %v: typing a search, last line %q, want its keys", focus, lines[len(lines)-1])
		}
	}
}

func TestPageNoBoardBehind(t *testing.T) {
	m := pageModel(t, 120, 40, FocusSettings)
	view := ansi.Strip(m.View())
	for _, s := range layoutSessions(goldenNow) {
		if strings.Contains(view, s.Slug) {
			t.Errorf("settings page shows session %q from the board", s.Slug)
		}
	}
}

func TestPageEscKeepsSelection(t *testing.T) {
	for _, focus := range []Focus{FocusSettings, FocusHelp} {
		for _, k := range []string{"esc", "q"} {
			m := layoutModel(goldenNow, 120, layoutSessions(goldenNow)...)
			m.Height = 40
			m.SelectedID = "3"
			m = openPage(t, m, focus)
			m = sendKey(m, key(k))
			if m.Focus != FocusBoard || m.SelectedID != "3" {
				t.Errorf("focus %v, %s: focus %v selected %q, want board and 3", focus, k, m.Focus, m.SelectedID)
			}
		}
	}
}

func TestPageQInertWhenOff(t *testing.T) {
	for _, focus := range []Focus{FocusSettings, FocusHelp} {
		m := overlayMode(t, layoutModel(goldenNow, 120, layoutSessions(goldenNow)...))
		m.Height = 40
		m = openPage(t, m, focus)
		m = sendKey(m, key("q"))
		if m.Focus != focus {
			t.Errorf("overlay %v: q moved focus to %v", focus, m.Focus)
		}
	}
}

func TestPageResize(t *testing.T) {
	m := pageModel(t, 120, 40, FocusSettings)
	// A late row of a long section, so the list has to scroll to keep it.
	m.Settings.CursorID = "audit_full_prompt"
	label := mustFind(t, m.Settings.CursorID).Label
	for _, sz := range []goldenSize{{50, 20}, {80, 24}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
		m = next.(Model)
		lines := viewLines(m)
		if len(lines) != sz.h {
			t.Errorf("%dx%d: %d lines", sz.w, sz.h, len(lines))
		}
		// The body only: the header's breadcrumb also names the row.
		if !strings.Contains(strings.Join(lines[3:], "\n"), label) {
			t.Errorf("%dx%d: cursor row %q not on screen", sz.w, sz.h, label)
		}
	}
}

func TestPageHeaderSlots(t *testing.T) {
	m := pageModel(t, 120, 40, FocusSettings)
	head := viewLines(m)[1]
	for _, want := range []string{"Appearance › Theme", "config.yaml"} {
		if !strings.Contains(head, want) {
			t.Errorf("settings header %q lacks %q", head, want)
		}
	}
	h := pageModel(t, 120, 40, FocusHelp)
	if head := viewLines(h)[1]; !strings.Contains(head, "rex "+version.Version) {
		t.Errorf("help header %q lacks the version", head)
	}

	m = pageModel(t, 50, 20, FocusSettings)
	if head := viewLines(m)[1]; strings.Contains(head, "config.yaml") {
		t.Errorf("narrow settings header %q keeps the right slot", head)
	}
	h = pageModel(t, 50, 20, FocusHelp)
	if head := viewLines(h)[1]; strings.Contains(head, "rex "+version.Version) {
		t.Errorf("narrow help header %q keeps the right slot", head)
	}
}

// On a short terminal every row, the last of a section included, stays on
// screen when the cursor is on it: the detail pane gives way first.
func TestPageShortTerminalCursorVisible(t *testing.T) {
	for _, sz := range []goldenSize{{80, 10}, {80, 12}, {50, 12}, {50, 14}, {120, 11}} {
		m := pageModel(t, sz.w, sz.h, FocusSettings)
		for _, s := range settingsList(&SettingsState{}) {
			m.Settings.CursorID = s.ID
			lines := viewLines(m)
			if len(lines) != sz.h {
				t.Fatalf("%dx%d: %d lines", sz.w, sz.h, len(lines))
			}
			prefix := string([]rune(s.Label)[:min(8, len([]rune(s.Label)))])
			if !strings.Contains(strings.Join(lines[3:sz.h-2], "\n"), "▸ "+prefix) {
				t.Errorf("%dx%d: cursor row %q not on screen:\n%s", sz.w, sz.h, s.ID, strings.Join(lines, "\n"))
			}
		}
	}
}

// A narrow settings page has no pane to tab to, so its footer leaves tab out.
func TestSettingsPageNarrowHintsNoTab(t *testing.T) {
	m := pageModel(t, 50, 20, FocusSettings)
	lines := viewLines(m)
	if foot := lines[len(lines)-1]; strings.Contains(foot, "tab") {
		t.Errorf("narrow footer offers tab: %q", foot)
	}
	m = pageModel(t, 120, 40, FocusSettings)
	lines = viewLines(m)
	if foot := lines[len(lines)-1]; !strings.Contains(foot, "tab sections") {
		t.Errorf("wide footer lacks tab: %q", foot)
	}
}
