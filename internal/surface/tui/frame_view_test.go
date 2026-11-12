package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

// framedBoard is a board on a fixed clock with window_border on (or off) and
// the terminal sized through a real WindowSizeMsg.
func framedBoard(t *testing.T, on bool, w, h int) Model {
	t.Helper()
	m := goldenBoard(t, goldenSize{w, h}, false)
	require.NoError(t, m.Store.Set("window_border", on))
	m.Width, m.Height = 0, 0
	got, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return got.(Model)
}

func viewLines(m Model) []string {
	return strings.Split(ansi.Strip(m.View()), "\n")
}

func TestFrameGeometry(t *testing.T) {
	m := framedBoard(t, true, 120, 40)
	require.Equal(t, 118, m.Width)
	require.Equal(t, 38, m.Height)
	lines := viewLines(m)
	require.Len(t, lines, 40)
	for i, l := range lines {
		require.Equal(t, 120, ansi.StringWidth(l), "line %d", i)
	}
	require.Equal(t, "█"+strings.Repeat("▀", 118)+"█", lines[0])
	require.Equal(t, "█"+strings.Repeat("▄", 118)+"█", lines[39])
	for _, l := range lines[1:39] {
		require.True(t, strings.HasPrefix(l, "█") && strings.HasSuffix(l, "█"), l)
	}
}

// Off is absent: the same view as a model that never heard of the frame.
func TestFrameOffChangesNothing(t *testing.T) {
	off := framedBoard(t, false, 100, 30)
	require.Equal(t, 100, off.Width)
	require.Equal(t, 30, off.Height)
	bare := off
	bare.TermW, bare.TermH = 0, 0
	require.Equal(t, bare.View(), off.View())
	require.NotContains(t, off.View(), "█")
}

func TestFrameMouseTranslation(t *testing.T) {
	on := framedBoard(t, true, 120, 40)
	off := framedBoard(t, false, 118, 38)
	require.Equal(t, on.Width, off.Width)
	selected := map[string]bool{}
	for y := range 38 {
		a, _ := updateMouse(off, click(10, y))
		viaFrame, _ := on.Update(click(11, y+1))
		require.Equal(t, a.SelectedID, viaFrame.(Model).SelectedID, "row %d", y)
		selected[a.SelectedID] = true
	}
	require.Greater(t, len(selected), 1, "the clicks must reach different rows")

	// Clicks on the frame are dropped.
	for _, p := range [][2]int{{0, 5}, {119, 5}, {10, 0}, {10, 39}} {
		got, cmd := on.Update(click(p[0], p[1]))
		require.Equal(t, on.SelectedID, got.(Model).SelectedID, "%v", p)
		require.Nil(t, cmd, "%v", p)
	}
}

func TestFrameAutoHideRules(t *testing.T) {
	// Splash: full-bleed, then the frame appears at hand-off.
	m := framedBoard(t, true, 120, 40)
	m.Focus = FocusBoot
	m = syncLayout(m)
	require.Equal(t, 120, m.Width)
	require.Equal(t, 40, m.Height)
	m, _ = m.handOffToBoard()
	m = syncLayout(m)
	require.Equal(t, 118, m.Width)

	// Screensaver: no frame drawn, layout kept (an attached agent is not resized).
	m.saverOn = true
	require.False(t, frameShown(m))
	require.Equal(t, 118, syncLayout(m).Width)
	lines := viewLines(m)
	require.Len(t, lines, 40)
	for _, l := range lines {
		require.LessOrEqual(t, ansi.StringWidth(l), 120)
		require.False(t, strings.HasPrefix(l, "█"), l)
	}
	m.saverOn = false
	require.True(t, frameShown(m))

	// Terminals too small for the layout floor (48x14 inside) get no frame.
	for _, sz := range [][2]int{{40, 12}, {49, 30}, {80, 15}, {50, 15}} {
		s := framedBoard(t, true, sz[0], sz[1])
		require.False(t, frameShown(s), "%v", sz)
		require.Equal(t, sz[0], s.Width)
		require.Equal(t, sz[1], s.Height)
	}
	s := framedBoard(t, true, 50, 16)
	require.True(t, frameShown(s))
	require.Equal(t, 48, s.Width)
	require.Equal(t, 14, s.Height)
}

func TestFrameSettingTogglesLayoutLive(t *testing.T) {
	m := framedBoard(t, false, 120, 40)
	require.NoError(t, m.Store.Set("window_border", true))
	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = got.(Model)
	require.Equal(t, 118, m.Width, "the next message relays out")
	require.NoError(t, m.Store.Set("window_border", false))
	got, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	require.Equal(t, 120, got.(Model).Width)
}

func TestFrameNoColorMonochrome(t *testing.T) {
	m := framedBoard(t, true, 100, 30)
	withProfile(termenv.Ascii, func() {
		v := m.View()
		require.NotContains(t, v, "\x1b", "NO_COLOR: glyphs only")
		require.Contains(t, v, "█▀")
		require.Contains(t, v, "█▄")
	})
}

func TestFrameStyledUsesEdgeColor(t *testing.T) {
	m := framedBoard(t, true, 100, 30)
	m.borderRunColor = "tangerine"
	withProfile(termenv.TrueColor, func() {
		require.Contains(t, m.View(), "\x1b[38;2;221;65;36m", "#DD4124")
	})
}

func TestBorderColorChoice(t *testing.T) {
	m := framedBoard(t, true, 100, 30)
	m.BorderSeed = "/work/proj"
	dark := borderPalette(m).Dark
	want := theme.PickEdge("/work/proj", dark, nil)
	require.Equal(t, want, borderEdge(m, dark), "random is stable for the launch folder")
	require.Equal(t, want, borderEdge(m, dark))

	require.NoError(t, m.Store.Set("window_border_color", "emerald"))
	require.Equal(t, "emerald", borderEdge(m, dark).ID)
	m.borderRunColor = "#ff00ff"
	require.Equal(t, "#ff00ff", borderEdge(m, dark).ID, "the run's choice wins")
	m.borderRunColor = "garbage"
	require.Equal(t, want, borderEdge(m, dark), "an invalid id never reaches a color: random applies")
	m.borderRunColor = "random"
	require.Equal(t, want, borderEdge(m, dark))

	// Light scheme draws the light variant; ansi uses the terminal index.
	m.borderRunColor = "mimosa"
	require.NoError(t, m.Store.Set("theme", "paper"))
	require.Equal(t, lipgloss.Color("#B18513"), borderColorOf(m))
	require.NoError(t, m.Store.Set("theme", "ansi"))
	require.Equal(t, lipgloss.Color("3"), borderColorOf(m))
	m.borderRunColor = "#ff00ff"
	require.Equal(t, lipgloss.Color("#ff00ff"), borderColorOf(m), "a custom hex has no ANSI index")

	m = Model{}
	require.False(t, borderEnabled(m))
	require.Contains(t, theme.EdgeIDs(), borderEdge(m, true).ID, "no store, no seed")
	on := true
	m.borderRunOn = &on
	require.True(t, borderEnabled(m))
}

func TestFrameRowsAreCached(t *testing.T) {
	a := frameRowsFor(20, lipgloss.Color("#ff0000"))
	require.Equal(t, a, frameRowsFor(20, lipgloss.Color("#ff0000")))
	require.NotEqual(t, a.top, frameRowsFor(21, lipgloss.Color("#ff0000")).top)
}

func TestWrapFramePadsAndCutsBody(t *testing.T) {
	m := Model{TermW: 52, TermH: 18, Width: 5, Height: 3}
	out := ansi.Strip(wrapFrame(m, "ab\nabcdefgh"))
	require.Equal(t, strings.Join([]string{
		"█▀▀▀▀▀█", "█ab   █", "█abcde█", "█     █", "█▄▄▄▄▄█"}, "\n"), out)
}

// Every view fills exactly the terminal, framed or not, down to tiny sizes.
func TestFrameEveryView(t *testing.T) {
	for _, sz := range []goldenSize{{120, 40}, {80, 24}, {60, 20}, {50, 16}, {40, 12}} {
		for _, sc := range goldenScreens() {
			m := sc.model(t, sz)
			require.NoError(t, m.Store.Set("window_border", true))
			m.TermW, m.TermH = sz.w, sz.h
			m = syncLayout(m)
			lines := viewLines(m)
			require.Len(t, lines, sz.h, "%s %dx%d", sc.name, sz.w, sz.h)
			for i, l := range lines {
				require.LessOrEqual(t, ansi.StringWidth(l), sz.w, "%s %dx%d line %d", sc.name, sz.w, sz.h, i)
				if frameShown(m) {
					require.Equal(t, sz.w, ansi.StringWidth(l), "%s %dx%d line %d", sc.name, sz.w, sz.h, i)
				}
			}
		}
	}
}

func TestFrameToggleResizesAttach(t *testing.T) {
	m := bannerModel(t, 120, 40, bannerSession())
	m.TermW, m.TermH = 120, 40
	var got [][2]uint16
	m.Attach.resize = func(c, r uint16) error { got = append(got, [2]uint16{c, r}); return nil }
	c0, r0 := m.Attach.Cols, m.Attach.Rows

	require.NoError(t, m.Store.Set("window_border", true))
	m = syncLayout(m)
	require.Equal(t, 118, m.Width)
	require.Equal(t, 38, m.Height)
	require.Len(t, got, 1)
	require.Less(t, m.Attach.Cols, c0)
	require.Less(t, m.Attach.Rows, r0)
	require.Equal(t, [2]uint16{uint16(m.Attach.Cols), uint16(m.Attach.Rows)}, got[0]) //nolint:gosec // G115: test sizes are small.

	require.NoError(t, m.Store.Set("window_border", false))
	m = syncLayout(m)
	require.Len(t, got, 2)
	require.Equal(t, c0, m.Attach.Cols)
	require.Equal(t, r0, m.Attach.Rows)
}

// The popup's terminal origin is in layout coordinates; on screen it is one
// cell right and down while the frame is shown.
func TestFrameAttachInnerOrigin_MatchesRenderedPopup(t *testing.T) {
	for _, sz := range [][2]int{{120, 40}, {101, 33}} {
		m := attachWithTerm(t, sz[0], sz[1], "Q")
		m.Store = settings.NewStore()
		m.Attach.resize = func(uint16, uint16) error { return nil }
		require.NoError(t, m.Store.Set("window_border", true))
		m.TermW, m.TermH = sz[0], sz[1]
		m = syncLayout(m)
		ox, oy := attachInnerOrigin(m)
		lines := strings.Split(ansi.Strip(m.View()), "\n")
		require.Len(t, lines, sz[1])
		require.Equal(t, "Q", ansi.Cut(lines[oy+1], ox+1, ox+2), "size %v: origin (%d,%d)", sz, ox, oy)
		// A click on the popup's first cell, in screen coordinates, reaches it as (0,0).
		tm, ok := translateMouse(m, click(ox+1, oy+1))
		require.True(t, ok)
		require.Equal(t, ox, tm.X)
		require.Equal(t, oy, tm.Y)
	}
}

// `rex render` builds a model with Width and Height and no WindowSizeMsg.
func TestRenderWithoutTerminalNeverFrames(t *testing.T) {
	m := goldenBoard(t, goldenSize{100, 30}, false)
	require.NoError(t, m.Store.Set("window_border", true))
	require.Zero(t, m.TermW)
	require.False(t, frameShown(m))
	require.NotContains(t, m.View(), "█")
	require.Equal(t, 100, m.Width)
}

func TestProjectRoot(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	require.NoError(t, os.MkdirAll(sub, 0o750))
	require.Equal(t, sub, projectRoot(sub), "no repository: the folder itself")
	require.NoError(t, os.Mkdir(filepath.Join(root, ".git"), 0o750))
	require.Equal(t, root, projectRoot(sub))
	require.Equal(t, root, projectRoot(root))

	t.Chdir(sub)
	got, err := filepath.EvalSymlinks(launchFolder())
	require.NoError(t, err)
	want, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	require.Equal(t, want, got)
}
