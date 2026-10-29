package tui

import (
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/features/audio"
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
		if hint, ok := bootFixHint(m); ok {
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

// bootSequence is the ordered boot pipeline, set by splash_steps.go's init.
// Steps run one at a time, separated by bootInterStep ticks.
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

// bootFixHint returns the next step for the most recent failed step. The
// PATH hint fits a daemon start failure only when there was no binary to
// run; otherwise the daemon ran and died or hung, and its log says why.
func bootFixHint(m Model) (string, bool) {
	step := lastFailedStep(m)
	if step == "daemon" && !daemonBinaryMissing(m.BootError) {
		return "see daemon.log (rex daemon logs)", true
	}
	hint, ok := bootFixHints[step]
	return hint, ok
}

// daemonBinaryMissing reports whether a daemonctl.Start error means the
// rex-daemon executable wasn't found.
func daemonBinaryMissing(err error) bool {
	return errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist)
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
		// Voice self-schedules after the startup tone and no-ops when boot_voice is off.
		m.Audio.Play(audio.EventBootVoice)
	}
	cmds := []tea.Cmd{tea.HideCursor}
	var c tea.Cmd
	if m, c = syncMouse(m, "board"); c != nil {
		cmds = append(cmds, c)
	}
	if m, c = armSpinner(m); c != nil {
		cmds = append(cmds, c)
	}
	if m.Client != nil {
		cmds = append(cmds, listenDaemon(m.Client))
	}
	return m, tea.Batch(cmds...)
}
