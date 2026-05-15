package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// bootLine is one rendered row in the splash log.
type bootLine struct {
	Name   string
	Status bootStatus
	Desc   string
	Err    error
}

// bootStatus is the status token for a row.
type bootStatus int

const (
	stepOK bootStatus = iota
	stepFail
	stepWarn
	stepSkip
)

// Splash tuning constants.
const (
	bootMinDuration = 1200 * time.Millisecond
	bootInterStep   = 70 * time.Millisecond
	bootCategoryW   = 18 // fixed column width for the category name
)

// bootStepMsg is emitted by a step Cmd when it finishes.
type bootStepMsg struct {
	Name   string
	Status bootStatus
	Desc   string
	Dur    time.Duration
	Err    error
}

// bootMinElapsedMsg is the min-duration tick.
type bootMinElapsedMsg struct{}

// bootMinTick returns a Cmd that posts bootMinElapsedMsg after bootMinDuration.
func bootMinTick() tea.Cmd {
	return tea.Tick(bootMinDuration, func(time.Time) tea.Msg { return bootMinElapsedMsg{} })
}

// stepFunc builds a step Cmd given the current model snapshot.
type stepFunc func(m Model) tea.Cmd
