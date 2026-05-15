// Package tui is the Rex Bubble Tea interface.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/audio"
	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/settings"
)

// boardPad is the side margin we leave so content isn't flush against the terminal edges.
const boardPad = 0

func Run(socket string) error {
	c, err := client.Dial(socket)
	if err != nil {
		return fmt.Errorf("dial daemon: %w", err)
	}
	defer c.Close()

	snap, err := c.Hello("rex-tui")
	if err != nil {
		return fmt.Errorf("hello: %w", err)
	}
	if err := c.Subscribe(""); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	store := settings.NewStore()
	storePath := settings.DefaultPath()
	_ = store.Load(storePath)

	m := Model{
		Client:    c,
		Socket:    socket,
		Focus:     FocusBoard,
		Sessions:  snap.Sessions,
		Filter:    "all",
		Store:     store,
		StorePath: storePath,
	}

	if scheme, _ := store.Get("color_scheme").(string); scheme != "" {
		applyTheme(scheme)
	}

	soundEnabled, _ := store.Get("sound_enabled").(bool)
	soundset, _ := store.Get("soundset").(string)
	volume, _ := store.Get("master_volume").(float64)
	if soundset == "off" {
		soundEnabled = false
	}
	m.Audio = audio.New(audio.Config{Enabled: soundEnabled, Volume: volume, Soundset: soundset})
	m.Audio.Play(audio.EventStartup)

	if sel, filt, ok := LoadTUIState(); ok {
		m.SelectedID = sel
		if filt != "" {
			m.Filter = filt
		}
	}
	// Mouse motion intentionally disabled — trackpad gestures were triggering
	// stray events that scrolled the view. Click-to-select can come back later
	// once we have a row-coordinate-aware hit test.
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(tea.HideCursor, listenDaemon(m.Client), tickSpinner())
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

	switch m.Focus {
	case FocusWizard:
		return centerOverlay(w, h, renderWizard(m), renderFullScreen(m, w, h))
	case FocusHelp:
		return centerOverlay(w, h, renderHelp(), renderFullScreen(m, w, h))
	case FocusSettings:
		return centerOverlay(w, h, renderSettings(m), renderFullScreen(m, w, h))
	case FocusAttach:
		return centerOverlay(w, h, renderAttach(m), renderFullScreen(m, w, h))
	case FocusFail:
		return centerOverlay(w, h, renderFail(m), renderFullScreen(m, w, h))
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
	default:
		bottom = hr + "\n" + renderPrompt(m, cw) + "\n" + helpline
	}

	// Fixed structure: 1 blank + 2 header + 1 blank + 1 hr + 1 blank + boardH + 3 bottom = h
	const fixedRows = 9
	boardH := h - fixedRows
	if boardH < 4 {
		boardH = 4
	}

	board := renderBoard(m, cw, boardH)

	lines := []string{""}
	lines = append(lines, strings.Split(header, "\n")...)
	lines = append(lines, "", hr, "")
	lines = append(lines, strings.Split(board, "\n")...)
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
		plW := ansi.StringWidth(pl)
		if plW < popupW {
			pl += strings.Repeat(" ", popupW-plW)
		}
		bgLines[row] = left + pl + right
	}

	return strings.Join(bgLines, "\n")
}
