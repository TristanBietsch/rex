// Package tui is the Rex Bubble Tea interface.
package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

	m := Model{
		Client:   c,
		Focus:    FocusBoard,
		Sessions: snap.Sessions,
		Filter:   "all",
	}

	store := settings.NewStore()
	_ = store.Load(settings.DefaultPath())
	soundEnabled, _ := store.Get("sound_enabled").(bool)
	soundset, _ := store.Get("soundset").(string)
	volume, _ := store.Get("master_volume").(float64)
	if soundset == "off" {
		soundEnabled = false
	}
	m.Audio = audio.New(audio.Config{Enabled: soundEnabled, Volume: volume})
	m.Audio.Play(audio.EventStartup)

	if sel, filt, ok := LoadTUIState(); ok {
		m.SelectedID = sel
		if filt != "" {
			m.Filter = filt
		}
	}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = p.Run()
	return err
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(listenDaemon(m.Client), tickSpinner())
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
	case FocusModal:
		return centerOverlay(w, h, renderModal(m), renderFullScreen(m, w, h))
	case FocusWizard:
		return centerOverlay(w, h, renderWizard(m), renderFullScreen(m, w, h))
	case FocusHelp:
		return centerOverlay(w, h, renderHelp(), renderFullScreen(m, w, h))
	case FocusSettings:
		return centerOverlay(w, h, renderSettings(m), renderFullScreen(m, w, h))
	case FocusSlash:
		return centerOverlay(w, h, renderSlash(m), renderFullScreen(m, w, h))
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

	var bottom string
	if m.Focus == FocusConfirmQuit {
		bottom = hr + "\n" + renderQuitConfirm(cw) + "\n" + helpline
	} else {
		bottom = hr + "\n" + renderPrompt(m, cw) + "\n" + helpline
	}

	headerLines := lipgloss.Height(header)
	bottomLines := lipgloss.Height(bottom)
	boardH := h - headerLines - bottomLines - 4
	if boardH < 4 {
		boardH = 4
	}

	board := renderBoard(m, cw, boardH)
	blank := strings.Repeat(" ", cw)

	inner := strings.Join([]string{
		blank,
		header,
		blank,
		hr,
		blank,
		board,
		bottom,
	}, "\n")

	// Center horizontally within the terminal.
	return horizCenter(inner, cw, w, h)
}

// horizCenter centers each line of `content` (width cw) within `tw` terminal cols,
// then pads/truncates total height to `th`.
func horizCenter(content string, cw, tw, th int) string {
	pad := (tw - cw) / 2
	if pad < 0 {
		pad = 0
	}
	padStr := strings.Repeat(" ", pad)
	lines := strings.Split(content, "\n")
	for i, ln := range lines {
		lines[i] = padStr + ln
	}
	for len(lines) < th {
		lines = append(lines, strings.Repeat(" ", tw))
	}
	if len(lines) > th {
		lines = lines[:th]
	}
	return strings.Join(lines, "\n")
}

// centerOverlay places `content` in a bordered popup centered on a w×h field.
// No background fill — the terminal's native bg shows around it.
func centerOverlay(w, h int, content, _ string) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(colorBorder).
		Padding(1, 2).
		Render(content)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box)
}
