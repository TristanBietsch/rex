package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

func runBorderLine(t *testing.T, m Model, line string) Model {
	t.Helper()
	withProfile(termenv.TrueColor, func() {
		m, _ = executeCommand(m, line)
		got, _ := m.Update(struct{}{}) // any message: Update re-lays out
		m = got.(Model)
	})
	return m
}

func TestBorderCommand(t *testing.T) {
	m := framedBoard(t, false, 120, 40)
	m.BorderSeed = "/work/proj"
	dark := borderPalette(m).Dark
	random := theme.PickEdge("/work/proj", dark, nil)

	// A bare :border turns the frame on for this run and says which color.
	m = runBorderLine(t, m, "border")
	require.True(t, frameShown(m))
	require.Equal(t, 118, m.Width)
	require.Equal(t, "border: "+random.ID+" (random · :border next)", m.Notice)
	require.Empty(t, m.Err)

	// ...and off again.
	m = runBorderLine(t, m, "border")
	require.False(t, frameShown(m))
	require.Equal(t, 120, m.Width)
	require.Equal(t, "border: off", m.Notice)

	m = runBorderLine(t, m, "border tangerine")
	require.True(t, frameShown(m))
	require.Equal(t, "tangerine", m.borderRunColor)
	require.Equal(t, "border: tangerine", m.Notice)

	m = runBorderLine(t, m, "border #FF00ff")
	require.Equal(t, "#ff00ff", m.borderRunColor)

	m = runBorderLine(t, m, "border random")
	require.Equal(t, random.ID, borderEdge(m, dark).ID)

	// next walks the pool from the current color.
	m = runBorderLine(t, m, "border next")
	require.Equal(t, theme.NextEdge(random.ID, dark).ID, m.borderRunColor)
	require.Equal(t, "border: "+m.borderRunColor, m.Notice)
	m = runBorderLine(t, m, "border next")
	require.Equal(t, theme.NextEdge(theme.NextEdge(random.ID, dark).ID, dark).ID, m.borderRunColor)

	m = runBorderLine(t, m, "border off")
	require.False(t, frameShown(m))
	m = runBorderLine(t, m, "border on")
	require.True(t, frameShown(m))
}

func TestBorderCommandRefusesBadArguments(t *testing.T) {
	m := framedBoard(t, false, 120, 40)
	for _, line := range []string{"border nope", "border \x1b[31m", "border #12", "border on off"} {
		got, _ := executeCommand(m, line)
		require.Contains(t, got.Err, "border: use :border [on|off|next|random|<color>|#rrggbb]", line)
		require.Contains(t, got.Err, "tangerine", line)
		require.Nil(t, got.borderRunOn, "%q changes nothing", line)
		require.Empty(t, got.borderRunColor, line)
	}
}

// :border is a per-run choice: it never writes the settings.
func TestBorderCommandIsNotPersisted(t *testing.T) {
	m := framedBoard(t, false, 120, 40)
	m.StorePath = t.TempDir() + "/config.yaml"
	m = runBorderLine(t, m, "border emerald")
	require.Equal(t, false, m.Store.Get("window_border"))
	require.Equal(t, "random", m.Store.Get("window_border_color"))
	require.NoFileExists(t, m.StorePath)
}

// The command works whatever window_border says, and off beats an enabled setting.
func TestBorderCommandOverridesSetting(t *testing.T) {
	m := framedBoard(t, true, 120, 40)
	require.True(t, frameShown(m))
	m = runBorderLine(t, m, "border off")
	require.False(t, frameShown(m))
	require.Equal(t, true, m.Store.Get("window_border"))
}

func TestBorderNoticeExplainsHiddenAndNoColor(t *testing.T) {
	m := framedBoard(t, false, 40, 12)
	m = runBorderLine(t, m, "border on")
	require.False(t, frameShown(m))
	require.Contains(t, m.Notice, "hidden: needs a 50x16 terminal")

	m = framedBoard(t, false, 120, 40)
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	defer lipgloss.SetColorProfile(prev)
	m, _ = executeCommand(m, "border on")
	require.Contains(t, m.Notice, "NO_COLOR is set")
	require.Contains(t, m.View(), "█▀", "still drawn")
}

func TestBorderIsInHelp(t *testing.T) {
	c, ok := lookupCommand("border")
	require.True(t, ok)
	require.NotEmpty(t, c.Desc)
	require.Empty(t, c.Flag, "no feature gate: :border works whatever window_border says")
}

// ::border works in the attach popup without detaching, and resizes the agent.
func TestAttachBorderCommand(t *testing.T) {
	m := bannerModel(t, 120, 40, bannerSession())
	m.TermW, m.TermH = 120, 40
	var got [][2]uint16
	m.Attach.resize = func(c, r uint16) error { got = append(got, [2]uint16{c, r}); return nil }
	c0 := m.Attach.Cols

	withProfile(termenv.TrueColor, func() { m, _ = executeAttachCommand(m, "border emerald") })
	next, _ := m.Update(struct{}{})
	m = next.(Model)
	require.Equal(t, FocusAttach, m.Focus, "the popup stays open")
	require.NotNil(t, m.Attach)
	require.True(t, frameShown(m))
	require.Equal(t, "border: emerald", m.Notice)
	require.Less(t, m.Attach.Cols, c0)
	require.NotEmpty(t, got)

	withProfile(termenv.TrueColor, func() { m, _ = executeAttachCommand(m, "border off") })
	next, _ = m.Update(struct{}{})
	m = next.(Model)
	require.Equal(t, FocusAttach, m.Focus)
	require.False(t, frameShown(m))
	require.Equal(t, c0, m.Attach.Cols)
}
