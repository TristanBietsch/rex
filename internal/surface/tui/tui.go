// Package tui is the Rex Bubble Tea interface.
package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/features/audio"
)

// boardPad is the side margin we leave so content isn't flush against the terminal edges.
const boardPad = 0

func Run(socket string) error {
	// Synchronous pre-bootstrap: read audio prefs so the Player exists by the
	// time the first step msg arrives. Best-effort; defaults on error.
	store := settings.NewStore()
	_ = store.Load(settings.DefaultPath())
	soundEnabled, _ := store.Get("sound_enabled").(bool)
	soundset, _ := store.Get("soundset").(string)
	volume, _ := store.Get("master_volume").(float64)
	bootVoice, _ := store.Get("boot_voice").(bool)
	if soundset == "off" {
		soundEnabled = false
	}
	player := audio.New(audio.Config{Enabled: soundEnabled, Volume: volume, Soundset: soundset, BootVoice: bootVoice})

	m := Model{
		Socket:    socket,
		Focus:     FocusBoot,
		Filter:    "all",
		Audio:     player,
		BootStart: time.Now(),
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return err
	}
	if fm, ok := final.(Model); ok && fm.BootFailed {
		return fm.BootError
	}
	return nil
}

func (m Model) Init() tea.Cmd {
	if m.Focus == FocusBoot {
		first := nextStep(m)
		return tea.Batch(tea.HideCursor, bootMinTick(), delayThen(first), tickSpinner(m.spinnerGen))
	}
	return tea.Batch(tea.HideCursor, listenDaemon(m.Client), tickSpinner(m.spinnerGen))
}

func (m Model) View() string {
	if m.Quitting {
		return ""
	}
	w, h := screenSize(m)

	switch m.Focus {
	case FocusBoot:
		return renderSplash(m, w, h)
	case FocusWizard:
		return centerOverlay(w, h, renderWizard(m), renderFullScreen(m, w, h))
	case FocusHelp:
		return centerOverlay(w, h, renderHelp(m), renderFullScreen(m, w, h))
	case FocusSettings:
		return centerOverlay(w, h, renderSettings(m), renderFullScreen(m, w, h))
	case FocusAttach:
		return centerOverlay(w, h, renderAttach(m), renderFullScreen(m, w, h))
	case FocusFail:
		return centerOverlay(w, h, renderFail(m), renderFullScreen(m, w, h))
	case FocusStats:
		return centerOverlay(w, h, renderStatsOverlay(m), renderFullScreen(m, w, h))
	case FocusCommand:
		// A command bar opened from inside the attach popup (::) keeps the
		// popup on screen; the bar renders in its footer.
		if m.Attach != nil {
			return centerOverlay(w, h, renderAttach(m), renderFullScreen(m, w, h))
		}
	}

	return renderFullScreen(m, w, h)
}

// contentWidth returns the inner board content width (full terminal width).
func contentWidth(termWidth int) int {
	w := termWidth - 2*boardPad
	if w < 40 {
		return termWidth
	}
	return w
}

func renderFullScreen(m Model, w, h int) string {
	cw := contentWidth(w)

	header := renderHeader(m, cw)
	hr := renderHR(cw)
	helpline := renderHelpLine(m, cw)
	if m.Store != nil {
		if show, ok := m.Store.Get("show_help_bar").(bool); ok && !show {
			// Keep the row to preserve layout, but blank it out.
			helpline = padLine("", cw)
		}
	}

	var prompt string
	switch m.Focus {
	case FocusConfirmQuit:
		prompt = renderQuitConfirm(cw)
	case FocusConfirmDelete:
		prompt = renderDeleteConfirm(m, cw)
	case FocusRename:
		prompt = renderRenamePrompt(m, cw)
	default:
		prompt = renderPrompt(m, cw)
	}
	// The bottom rows always render, one line each: they used to be cut
	// first whenever anything above overflowed the screen.
	bottom := []string{hr, firstLine(prompt), firstLine(helpline)}

	// 1 blank + header + blank + hr + blank (boardTop rows) + board + 3 bottom
	// = h. The header keeps exactly headerHeight lines so mouse hit-testing
	// (boardTop) matches what is drawn.
	lines := make([]string, 0, h+len(bottom))
	lines = append(lines, "")
	headerLines := strings.Split(header, "\n")
	for i := range headerHeight(m) {
		if i < len(headerLines) {
			lines = append(lines, headerLines[i])
		} else {
			lines = append(lines, "")
		}
	}
	lines = append(lines, "", hr, "")
	// The board absorbs any shortfall, down to no rows on a tiny screen.
	if boardH := boardHeightFor(m, h); boardH > 0 {
		lines = append(lines, strings.Split(renderBoard(m, cw, boardH), "\n")...)
	}
	// Too short even for the header: cut the top from below so the header
	// rows that do show stay where hit-testing expects them.
	if room := max(h-len(bottom), 0); len(lines) > room {
		lines = lines[:room]
	}
	lines = append(lines, bottom...)
	if len(lines) > h {
		lines = lines[len(lines)-h:]
	}
	// A line wider than the terminal wraps and pushes everything below it
	// down a row (rows narrower than the fixed columns on a tiny screen).
	for i, l := range lines {
		if ansi.StringWidth(l) > w {
			lines[i] = ansi.Truncate(l, w, "")
		}
	}
	return strings.Join(lines, "\n")
}

// firstLine is s up to its first newline: a bottom bar row stays one row
// even if a renderer lets a newline through.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// centerOverlay composites `content` as a bordered popup centered over `bg`.
// The board behind it stays visible around the popup so it reads as an
// overlay, not a separate screen.
func centerOverlay(w, h int, content, bg string) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorder).
		Padding(1, 2).
		Render(content)

	bgLines := strings.Split(bg, "\n")
	for len(bgLines) < h {
		bgLines = append(bgLines, "")
	}

	popupLines := strings.Split(box, "\n")
	popupW := 0
	for _, l := range popupLines {
		if pw := ansi.StringWidth(l); pw > popupW {
			popupW = pw
		}
	}
	// A popup wider than the screen is clipped: a line past the right edge
	// wraps and shifts every row below it.
	popupW = min(popupW, w)
	popupH := len(popupLines)

	x := (w - popupW) / 2
	if x < 0 {
		x = 0
	}
	y := (h - popupH) / 2
	if y < 0 {
		y = 0
	}

	for i, pl := range popupLines {
		row := y + i
		if row >= len(bgLines) {
			break
		}
		bgLine := bgLines[row]
		// Exact columns on both sides: a wide rune of the board straddling
		// an edge used to shift the popup row (left cut) or widen the line
		// (right cut) by one column.
		bgLines[row] = fitColumns(bgLine, x) + fitColumns(pl, popupW) + cutLeft(bgLine, x+popupW)
	}

	return strings.Join(bgLines, "\n")
}

// cutLeft drops the first n columns of s and keeps exactly the width that
// remains; a wide rune straddling column n becomes a space (ansi.TruncateLeft
// keeps it, one column too many).
func cutLeft(s string, n int) string {
	want := ansi.StringWidth(s) - n
	if want <= 0 {
		return ""
	}
	r := ansi.TruncateLeft(s, n, "")
	if ansi.StringWidth(r) > want {
		r = " " + ansi.TruncateLeft(s, n+1, "")
	}
	return r
}
