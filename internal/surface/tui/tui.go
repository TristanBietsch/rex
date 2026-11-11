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
	store, _, _ := settings.LoadDefault()
	soundEnabled, _ := store.Get("sound_enabled").(bool)
	soundset, _ := store.Get("soundset").(string)
	volume, _ := store.Get("master_volume").(float64)
	bootVoice, _ := store.Get("boot_voice").(bool)
	if soundset == "off" {
		soundEnabled = false
	}
	player := audio.New(audio.Config{Enabled: soundEnabled, Volume: volume, Soundset: soundset, BootVoice: bootVoice})

	awake := newDisplayAwake()
	idleDone := make(chan struct{})
	m := Model{
		Socket:    socket,
		Focus:     FocusBoot,
		Filter:    "all",
		Audio:     player,
		BootStart: time.Now(),
		idleDone:  idleDone,
		keepAwake: awake,
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	return runProgram(p.Run, awake, idleDone)
}

// runProgram runs the TUI and, however it ends (quit, error, a signal, a
// panic Bubble Tea recovered), lets the display sleep again and ends the
// screensaver's pending idle timer.
func runProgram(run func() (tea.Model, error), awake *displayAwake, idleDone chan struct{}) error {
	defer close(idleDone)
	defer awake.Stop()
	final, err := run()
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
	w, h := m.Width, m.Height
	if w <= 0 {
		w = 100
	}
	if h <= 0 {
		h = 32
	}
	return clipWidth(m.view(w, h), w)
}

// clipWidth is the last guard: no line leaves View wider than the terminal,
// whatever an overlay's minimum size is on a tiny screen.
func clipWidth(s string, w int) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if ansi.StringWidth(l) > w {
			lines[i] = ansi.Truncate(l, w, "")
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) view(w, h int) string {
	if m.saverOn && m.Focus == FocusBoard {
		return renderScreensaver(m, w, h)
	}

	switch m.Focus {
	case FocusBoot:
		return renderSplash(m, w, h)
	case FocusWizard:
		return centerOverlay(w, h, fitOverlay(renderWizard(m), w), renderFullScreen(m, w, h))
	case FocusHelp:
		return centerOverlayBox(w, h, renderHelp(m, w, h), renderFullScreen(m, w, h))
	case FocusSettings:
		return centerOverlayBox(w, h, renderSettings(m, w), renderFullScreen(m, w, h))
	case FocusAttach:
		return centerOverlay(w, h, renderAttach(m), renderFullScreen(m, w, h))
	case FocusFail:
		return centerOverlay(w, h, fitOverlay(renderFail(m), w), renderFullScreen(m, w, h))
	case FocusStats:
		return centerOverlay(w, h, fitOverlay(renderStatsOverlay(m), w), renderFullScreen(m, w, h))
	case FocusOverview:
		return centerOverlay(w, h, fitOverlay(renderOverviewOverlay(m), w), renderFullScreen(m, w, h))
	case FocusCommand:
		// A command bar opened from inside the attach popup (::) keeps the
		// popup on screen; the bar renders in its footer.
		if m.Attach != nil {
			return centerOverlay(w, h, renderAttach(m), renderFullScreen(m, w, h))
		}
	default:
		// The board and its bottom-bar prompts render on the full screen.
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

	var bottom string
	switch m.Focus {
	case FocusConfirmQuit:
		bottom = hr + "\n" + renderQuitConfirm(cw) + "\n" + helpline
	case FocusConfirmDelete:
		bottom = hr + "\n" + renderDeleteConfirm(m, cw) + "\n" + helpline
	case FocusRename:
		bottom = hr + "\n" + renderRenamePrompt(m, cw) + "\n" + helpline
	default:
		bottom = hr + "\n" + renderPrompt(m, cw) + "\n" + helpline
	}

	// 1 blank + header + blank + hr + blank + board + preview + 3 bottom = h
	// (boardTop/boardHeightFor/boardBottomRows).
	boardH := boardHeightFor(m, h)
	m.Height = h

	board := renderBoard(m, cw, boardH)

	lines := []string{""}
	lines = append(lines, strings.Split(header, "\n")...)
	lines = append(lines, "", hr, "")
	lines = append(lines, strings.Split(board, "\n")...)
	lines = append(lines, renderDescPreview(m, cw)...)
	lines = append(lines, strings.Split(bottom, "\n")...)

	// Hard cap: never produce more than h lines (the alt-screen window).
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// centerOverlay composites `content` as a bordered popup centered over `bg`.
// The board behind it stays visible around the popup so it reads as an
// overlay, not a separate screen.
func centerOverlay(w, h int, content, bg string) string {
	popupLines, popupW := overlayBox(content)
	return compositeOverlay(w, h, popupLines, popupW, bg)
}

// overlayChrome is the columns overlayBox adds around its content: two for the
// border and four for the padding.
const overlayChrome = 6

// fitOverlay cuts every line of overlay content to what overlayBox can show
// on a w-column screen, so the popup is never wider than the terminal. Lines
// that already fit are untouched.
func fitOverlay(content string, w int) string {
	limit := max(w-overlayChrome, 1)
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		if ansi.StringWidth(l) > limit {
			lines[i] = ansi.Truncate(l, limit, "…")
		}
	}
	return strings.Join(lines, "\n")
}

// centerOverlayBox composites an already-rendered box centered over bg.
func centerOverlayBox(w, h int, box, bg string) string {
	popupLines := strings.Split(box, "\n")
	popupW := 0
	for _, l := range popupLines {
		popupW = max(popupW, ansi.StringWidth(l))
	}
	return compositeOverlay(w, h, popupLines, popupW, bg)
}

// compositeOverlay draws popupLines (each popupW wide) centered over bg.
func compositeOverlay(w, h int, popupLines []string, popupW int, bg string) string {
	bgLines := strings.Split(bg, "\n")
	for len(bgLines) < h {
		bgLines = append(bgLines, "")
	}
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
		bgW := ansi.StringWidth(bgLine)
		if bgW < x+popupW {
			bgLine += strings.Repeat(" ", x+popupW-bgW)
		}
		left := ansi.Truncate(bgLine, x, "")
		right := ansi.TruncateLeft(bgLine, x+popupW, "")
		bgLines[row] = left + pl + right
	}

	return strings.Join(bgLines, "\n")
}

// overlayBox draws content in a rounded border with one blank line above and
// below and two columns each side, every line padded to the same width. It
// matches lipgloss Border(RoundedBorder()).Padding(1, 2) but measures each
// line once; lipgloss measured them several times, which was most of the
// attach popup's frame time. It returns the box lines and their width.
func overlayBox(content string) ([]string, int) {
	content = strings.ReplaceAll(content, "\r\n", "\n") // as lipgloss does
	content = strings.ReplaceAll(content, "\t", "    ") // lipgloss's tab width
	lines := strings.Split(content, "\n")
	widths := make([]int, len(lines))
	inner := 0
	for i, l := range lines {
		widths[i] = ansi.StringWidth(l)
		inner = max(inner, widths[i])
	}
	padded := inner + 4
	border := lipgloss.NewStyle().Foreground(colorBorder)
	side := border.Render("│")
	blank := side + strings.Repeat(" ", padded) + side
	rule := strings.Repeat("─", padded)

	out := make([]string, 0, len(lines)+4)
	out = append(out, border.Render("╭"+rule+"╮"), blank)
	for i, l := range lines {
		out = append(out, side+"  "+l+strings.Repeat(" ", inner-widths[i])+"  "+side)
	}
	out = append(out, blank, border.Render("╰"+rule+"╯"))
	return out, padded + 2
}
