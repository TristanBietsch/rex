package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tristanbietsch/rex/internal/client"
	"github.com/tristanbietsch/rex/internal/protocol"
)

// DaemonEventMsg wraps a single event from the daemon.
type DaemonEventMsg struct {
	Env protocol.Envelope
}

// DaemonErrMsg is sent when the connection to the daemon dies.
type DaemonErrMsg struct {
	Err error
}

// SpinnerTickMsg fires periodically to drive the working-state spinner.
type SpinnerTickMsg struct{}

// listenDaemon returns a tea.Cmd that reads ONE event then re-arms itself.
func listenDaemon(c *client.Client) tea.Cmd {
	return func() tea.Msg {
		env, err := c.NextEvent()
		if err != nil {
			return DaemonErrMsg{Err: err}
		}
		return DaemonEventMsg{Env: env}
	}
}

// tickSpinner emits a SpinnerTickMsg every 100ms.
func tickSpinner() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return SpinnerTickMsg{} })
}
