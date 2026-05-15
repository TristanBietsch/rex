package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Update is the central Bubble Tea handler.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil
	case DaemonEventMsg:
		m = m.applyEvent(msg.Env)
		return m, listenDaemon(m.Client)
	case DaemonErrMsg:
		m.Err = msg.Err.Error()
		return m, tea.Quit
	case SpinnerTickMsg:
		m.SpinnerTick++
		return m, tickSpinner()
	case tea.KeyMsg:
		return updateKey(m, msg)
	}
	return m, nil
}

func updateKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	if m.Focus == FocusBoard {
		switch k.String() {
		case "ctrl+c", "q":
			m.Quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}
