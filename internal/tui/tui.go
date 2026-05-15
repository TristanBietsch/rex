// Package tui is the Rex Bubble Tea interface.
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/client"
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

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(listenDaemon(m.Client), tickSpinner())
}

// View satisfies tea.Model.
func (m Model) View() string {
	if m.Quitting {
		return ""
	}
	if m.Focus == FocusModal {
		return renderModal(m)
	}
	if m.Focus == FocusWizard {
		return renderWizard(m)
	}
	base := renderHeader(m) + "\n\n" + renderBoard(m) + renderPrompt(m) + "\n" + renderHelpLine(m) + "\n"
	if m.Focus == FocusConfirmQuit {
		return base + "\n" + renderQuitConfirm()
	}
	return base
}
