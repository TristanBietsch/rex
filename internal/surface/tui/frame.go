package tui

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

// Window frame. The terminal size (TermW x TermH) is kept apart from the
// layout size (Width x Height): every view lays out in Width x Height, and the
// frame, when shown, is drawn around it by View, so no view needs frame code.
// The frame is block glyphs: "█" sides and corners, "▀" on top and "▄" at the
// bottom (half a cell tall each, so all four edges look equally thick).

// frameMinInnerW and frameMinInnerH are the smallest layout area that still
// gets a frame; on a smaller terminal the frame gives way.
const (
	frameMinInnerW = 48
	frameMinInnerH = 14
)

// borderEnabled is whether the user wants a frame: the per-run :border choice
// when there is one, else the window_border setting.
func borderEnabled(m Model) bool {
	if m.borderRunOn != nil {
		return *m.borderRunOn
	}
	if m.Store == nil {
		return false
	}
	on, _ := m.Store.Get("window_border").(bool)
	return on
}

// frameInset is how many cells the frame takes on each side: 1 when the
// layout leaves room for a frame, else 0. It ignores the screensaver on
// purpose: the screensaver hides the frame but keeps the layout, so an
// attached agent is not resized (SIGWINCH) when the screensaver comes and goes.
func frameInset(m Model) int {
	if !borderEnabled(m) || m.TermW <= 0 || m.Focus == FocusBoot {
		return 0
	}
	if m.TermW-2 < frameMinInnerW || m.TermH-2 < frameMinInnerH {
		return 0
	}
	return 1
}

// frameShown is whether View draws the frame now.
func frameShown(m Model) bool { return frameInset(m) > 0 && !m.saverOn }

// relayout recomputes the layout size from the terminal size and the frame,
// then refits the attach popup and the board scroll. A Model with TermW == 0
// (tests, `rex render`) keeps the Width and Height it was built with.
func relayout(m Model) Model {
	if m.TermW > 0 {
		inset := frameInset(m)
		m.Width = m.TermW - 2*inset
		m.Height = m.TermH - 2*inset
	}
	resizeAttach(m)
	return ensureVisible(m)
}

// syncLayout re-runs relayout when the frame turned on or off since the last
// message (a setting, :border, the splash handing off, a resize of the
// terminal under it). Update calls it once per message, so no branch has to.
func syncLayout(m Model) Model {
	if m.TermW <= 0 {
		return m
	}
	inset := frameInset(m)
	if m.Width == m.TermW-2*inset && m.Height == m.TermH-2*inset {
		return m
	}
	return relayout(m)
}

// translateMouse maps a mouse event from screen to layout coordinates. It
// reports false for an event on the frame (or outside the layout area).
func translateMouse(m Model, msg tea.MouseMsg) (tea.MouseMsg, bool) {
	inset := frameInset(m)
	if inset == 0 {
		return msg, true
	}
	ev := tea.MouseEvent(msg)
	x, y := ev.X-inset, ev.Y-inset
	if x < 0 || y < 0 || x >= m.Width || y >= m.Height {
		return msg, false
	}
	ev.X, ev.Y = x, y
	return tea.MouseMsg(ev), true
}

// borderPalette is the live color scheme's palette.
func borderPalette(m Model) theme.Palette {
	name := theme.DefaultName
	if m.Store != nil {
		if s, ok := m.Store.Get("color_scheme").(string); ok {
			name = s
		}
	}
	p, _ := theme.Get(name)
	return p
}

// borderEdge is the frame's color: the per-run choice, else window_border_color.
// "random" is stable for the launch folder.
func borderEdge(m Model, dark bool) theme.EdgeColor {
	id := m.borderRunColor
	if id == "" && m.Store != nil {
		id, _ = m.Store.Get("window_border_color").(string)
	}
	if id != "" && id != "random" {
		if c, err := theme.ParseEdge(id); err == nil {
			return c
		}
	}
	seed := m.BorderSeed
	if seed == "" {
		seed = "rex"
	}
	return theme.PickEdge(seed, dark, nil)
}

// borderColorOf is the terminal color for the frame. Only a palette id or a
// validated hex ever reaches lipgloss.Color. The ansi scheme (no background of
// its own) uses the color's terminal palette index.
func borderColorOf(m Model) lipgloss.Color {
	p := borderPalette(m)
	c := borderEdge(m, p.Dark)
	if p.Bg == "" && c.ANSI != "" {
		return lipgloss.Color(c.ANSI)
	}
	return lipgloss.Color(c.Hex(p.Dark))
}

// frameRows are the three styled strings a frame is built from.
type frameRows struct{ top, bottom, side string }

type frameKey struct {
	w       int
	color   lipgloss.Color
	profile termenv.Profile
}

// frameCache holds the rows of the last frame: they only change with the
// width, the color or the color profile, so a frame costs string concatenation.
var frameCache struct {
	mu   sync.Mutex
	key  frameKey
	rows frameRows
	ok   bool
}

func frameRowsFor(w int, color lipgloss.Color) frameRows {
	key := frameKey{w: w, color: color, profile: lipgloss.ColorProfile()}
	frameCache.mu.Lock()
	defer frameCache.mu.Unlock()
	if frameCache.ok && frameCache.key == key {
		return frameCache.rows
	}
	st := edgeStyle(color)
	frameCache.rows = frameRows{
		top:    st.Render("█" + strings.Repeat("▀", w) + "█"),
		bottom: st.Render("█" + strings.Repeat("▄", w) + "█"),
		side:   st.Render("█"),
	}
	frameCache.key, frameCache.ok = key, true
	return frameCache.rows
}

// wrapFrame draws the frame around body, which is the Width x Height layout
// area: every body line is padded or cut to Width and the body to Height, so
// the result is exactly TermH lines of TermW cells.
func wrapFrame(m Model, body string) string {
	rows := frameRowsFor(m.Width, borderColorOf(m))
	lines := strings.Split(body, "\n")
	var b strings.Builder
	b.Grow(len(body) + (m.Height+2)*(len(rows.side)*2+1) + len(rows.top) + len(rows.bottom))
	b.WriteString(rows.top)
	for i := range m.Height {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		b.WriteByte('\n')
		b.WriteString(rows.side)
		w := ansi.StringWidth(line)
		if w > m.Width {
			line = ansi.Truncate(line, m.Width, "")
			w = ansi.StringWidth(line)
		}
		b.WriteString(line)
		if w < m.Width {
			b.WriteString(strings.Repeat(" ", m.Width-w))
		}
		b.WriteString(rows.side)
	}
	b.WriteByte('\n')
	b.WriteString(rows.bottom)
	return b.String()
}

// launchFolder is the seed of the "random" border color: the git root when
// the TUI starts inside a repository, else the working directory, so a
// project keeps its color. "" when the working directory is unknown.
func launchFolder() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return projectRoot(wd)
}

func projectRoot(dir string) string {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
		d = parent
	}
}
