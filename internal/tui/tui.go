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

// Run launches the TUI. Blocks until the user quits.
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
		return renderModalFull(m, w, h)
	case FocusWizard:
		return centerOverlay(w, h, renderWizard(m))
	case FocusHelp:
		return centerOverlay(w, h, renderHelp())
	case FocusSettings:
		return centerOverlay(w, h, renderSettings(m))
	case FocusSlash:
		return centerOverlay(w, h, renderSlash(m))
	}

	return renderFullScreen(m, w, h)
}

// renderFullScreen builds the board view filling the terminal.
func renderFullScreen(m Model, w, h int) string {
	header := renderHeader(m, w)
	hr := renderHR(w)
	helpline := renderHelpLine(m, w)

	var bottom string
	if m.Focus == FocusConfirmQuit {
		bottom = hr + "\n" + renderQuitConfirm(w) + "\n" + helpline
	} else {
		bottom = hr + "\n" + renderPrompt(m, w) + "\n" + helpline
	}

	headerLines := lipgloss.Height(header)
	bottomLines := lipgloss.Height(bottom)
	// Header(2) + blank(1) + HR(1) + blank(1) + board + bottom(3) + blank(1)
	boardH := h - headerLines - bottomLines - 4
	if boardH < 4 {
		boardH = 4
	}

	board := renderBoard(m, w, boardH)
	blank := padLine("", w)

	out := strings.Join([]string{
		blank,
		header,
		blank,
		hr,
		blank,
		board,
		bottom,
	}, "\n")

	return fitHeight(out, w, h)
}

func fitHeight(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	for len(lines) < h {
		lines = append(lines, padLine("", w))
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

// centerOverlay places `content` inside a bordered box centered on a w×h field.
func centerOverlay(w, h int, content string) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(colorBorder).
		Padding(1, 2).
		Render(content)
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box)
}
