package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/audio"
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

// renderSplash renders the boot splash, full-screen, no overlay.
func renderSplash(m Model, w, h int) string {
	leftPad := "  "
	header := leftPad + styleBootHeader.Render("∴ レックス") +
		styleDim.Render(" · rex runtime executive")

	elapsed := time.Since(m.BootStart).Truncate(10 * time.Millisecond)
	statusLine := leftPad + renderSplashStatusLine(m, elapsed)

	rows := []string{
		"",
		header,
		statusLine,
		"",
	}
	for _, ln := range m.BootLog {
		rows = append(rows, leftPad+renderBootLine(ln))
	}
	if m.BootDone && !m.BootFailed {
		ready := fmt.Sprintf("準備完了 ready · %s", elapsed)
		rows = append(rows, "", lipgloss.PlaceHorizontal(w-2, lipgloss.Right, styleBootReady.Render(ready)))
	}
	if m.BootFailed {
		rows = append(rows, "")
		if m.BootError != nil {
			rows = append(rows, leftPad+styleDim.Render("cause: ")+styleBootCause.Render(m.BootError.Error()))
		}
		if hint, ok := bootFixHints[lastFailedStep(m)]; ok {
			rows = append(rows, leftPad+styleDim.Render("fix:   ")+stylePrimary.Render(hint))
		}
		rows = append(rows, "", lipgloss.PlaceHorizontal(w-2, lipgloss.Right, styleDim.Render("press q · ctrl+c to quit")))
	}
	for len(rows) < h {
		rows = append(rows, "")
	}
	if len(rows) > h {
		rows = rows[:h]
	}
	return strings.Join(rows, "\n")
}

// renderSplashStatusLine produces either the running header or, on failure,
// the freeze-state header.
func renderSplashStatusLine(m Model, elapsed time.Duration) string {
	if m.BootFailed {
		return styleBootFail.Render("起動失敗 boot failed")
	}
	return styleDim.Render(fmt.Sprintf("起動中 booting...  %s", elapsed))
}

// renderBootLine renders one log row: "[ STATUS ] <category> · <desc>".
func renderBootLine(ln bootLine) string {
	token := bootStatusToken(ln.Status)
	cat := padCategory(ln.Name)
	desc := ln.Desc
	return token + " " + stylePrimary.Render(cat) + "  " +
		styleDim.Render("·") + " " + stylePrimary.Render(desc)
}

// bootStatusToken returns the 8-char "[ XX ]" status fragment with color.
func bootStatusToken(s bootStatus) string {
	inner := func(text string, style lipgloss.Style) string {
		return styleBootBracket.Render("[") + " " + style.Render(text) + " " + styleBootBracket.Render("]")
	}
	switch s {
	case stepOK:
		return inner("  OK  ", styleBootOK)
	case stepFail:
		return inner(" FAIL ", styleBootFail)
	case stepWarn:
		return inner(" WARN ", styleBootWarn)
	case stepSkip:
		return inner(" SKIP ", styleBootSkip)
	}
	return inner("  ..  ", styleBootRun)
}

// padCategory left-pads the category name to bootCategoryW cells, east-asian-width aware.
func padCategory(name string) string {
	w := ansi.StringWidth(name)
	if w >= bootCategoryW {
		return name
	}
	return name + strings.Repeat(" ", bootCategoryW-w)
}

// bootSequence is populated by Stage 4 (real Cmds). Stage 3 uses a stub.
// Stage 3 stub builds 17 fake step msgs separated by bootInterStep ticks.
var bootSequence []stepFunc

// nextStep returns a Cmd that produces bootStepMsg #BootStep, then schedules
// the inter-step delay before the *next* step's Cmd. Returns nil when the
// pipeline is complete.
func nextStep(m Model) tea.Cmd {
	if m.BootStep >= len(bootSequence) {
		return nil
	}
	step := bootSequence[m.BootStep]
	return step(m)
}

// delayThen schedules a tea.Tick of bootInterStep, then dispatches inner.
// Used to space step messages so the splash cascades visibly.
func delayThen(inner tea.Cmd) tea.Cmd {
	return tea.Tick(bootInterStep, func(time.Time) tea.Msg {
		if inner == nil {
			return nil
		}
		return inner()
	})
}

// chimeFor maps a status to the audio event name; "" for SKIP (silent).
func chimeFor(s bootStatus) string {
	switch s {
	case stepOK:
		return audio.EventBootOK
	case stepWarn:
		return audio.EventBootWarn
	case stepFail:
		return audio.EventBootFail
	}
	return ""
}

// bootFixHints maps a failed step name to a human-readable next step.
var bootFixHints = map[string]string{
	"paths.ensure":   "check that you can write to ~/.config and ~/.local/state",
	"registry.load":  "rex's built-in YAML is corrupt — reinstall or rebuild rex",
	"socket.resolve": "set XDG_RUNTIME_DIR or ensure HOME is set",
	"daemon":         "ensure rex-daemon is on PATH or installed alongside rex",
	"client.dial":    "the daemon socket disappeared — try `rex daemon start`",
	"handshake":      "protocol mismatch — rebuild rex and rex-daemon to the same version",
	"subscribe":      "the daemon rejected the event subscription — check daemon.log",
}

// lastFailedStep returns the name of the most recent stepFail row.
func lastFailedStep(m Model) string {
	for i := len(m.BootLog) - 1; i >= 0; i-- {
		if m.BootLog[i].Status == stepFail {
			return m.BootLog[i].Name
		}
	}
	return ""
}

// handOffToBoard transitions from FocusBoot to FocusBoard, releases the boot
// log, plays the startup chime, and starts the daemon-event listener.
func (m Model) handOffToBoard() (Model, tea.Cmd) {
	m.Focus = FocusBoard
	m.BootLog = nil
	if m.Audio != nil {
		m.Audio.Play(audio.EventStartup)
	}
	cmds := []tea.Cmd{tea.HideCursor, tickSpinner()}
	if m.Client != nil {
		cmds = append(cmds, listenDaemon(m.Client))
	}
	return m, tea.Batch(cmds...)
}
