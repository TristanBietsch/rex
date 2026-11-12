package tui

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/features/audio"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Loop cards (the loops feature): a loop's session row carries a marker,
// "iter/max" and the loop's state; p pauses or resumes it, x stops it (after
// a y/N), L or :loops lists the loops. Everything here answers
// offMessage("loops") while the feature is off, and the rows show nothing.

// loopsFlag is the Features switch that gates loop cards, keys and commands.
const loopsFlag = "loops"

// loopCallTimeout bounds one loop intent.
const loopCallTimeout = 10 * time.Second

// loopController is the client surface the loop keys need. *client.Client
// satisfies it; tests inject a recorder.
type loopController interface {
	PauseLoop(ctx context.Context, loopID string) (protocol.LoopSummary, error)
	ResumeLoop(ctx context.Context, loopID string) (protocol.LoopSummary, error)
	StopLoop(ctx context.Context, loopID string) (protocol.LoopSummary, error)
}

// loopActionMsg is the daemon's answer to a loop key or command.
type loopActionMsg struct {
	Op   string // pause, resume or stop
	Loop protocol.LoopSummary
	Err  error
}

// looperOf mirrors retirerOf for the loop intents.
func looperOf(m Model) loopController {
	if m.looper != nil {
		return m.looper
	}
	if m.Client == nil {
		return nil
	}
	return m.Client
}

// adoptLoops takes the loops of a snapshot (replacing the list).
func adoptLoops(m Model, snap *protocol.Snapshot) Model {
	m.Loops = slices.Clone(snap.Loops)
	return m
}

// upsertLoop adds l or replaces the loop with its id.
func upsertLoop(m Model, l protocol.LoopSummary) Model {
	loops := slices.Clone(m.Loops)
	if i := slices.IndexFunc(loops, func(x protocol.LoopSummary) bool { return x.ID == l.ID }); i >= 0 {
		loops[i] = l
	} else {
		loops = append(loops, l)
	}
	m.Loops = loops
	return m
}

// loopByID finds a loop of the board.
func loopByID(m Model, id string) (protocol.LoopSummary, bool) {
	for _, l := range m.Loops {
		if l.ID == id {
			return l, true
		}
	}
	return protocol.LoopSummary{}, false
}

// pauseReasonText is a paused loop's reason in words.
var pauseReasonText = map[string]string{
	protocol.PauseUser:         "paused",
	protocol.PauseNeedsInput:   "paused: needs you",
	protocol.PauseUserInput:    "paused: you typed",
	protocol.PauseRestart:      "paused: daemon restarted",
	protocol.PauseSessionEnded: "paused: session ended",
	protocol.PauseStateChanged: "paused: state changed",
	protocol.PauseSendFailed:   "paused: send failed",
	protocol.PauseOff:          "paused: loops off",
}

// stopReasonText is a finished loop's reason in words.
var stopReasonText = map[string]string{
	protocol.StopCheckPassed:   "check passed",
	protocol.StopMarker:        "marker seen",
	protocol.StopMaxIterations: "max iterations",
	protocol.StopMaxMinutes:    "max minutes",
	protocol.StopByUser:        "stopped",
	protocol.StopCheckError:    "check did not run",
}

// loopStateText is the loop's state as a short phrase: running, paused: why,
// done, capped: which cap, failed, stopped. The reasons are a closed set of
// words rex wrote; anything else a daemon sends shows as the bare state.
func loopStateText(l protocol.LoopSummary) string {
	switch l.State {
	case protocol.LoopPaused:
		if t, ok := pauseReasonText[l.PauseReason]; ok {
			return t
		}
		return "paused"
	case protocol.LoopCapped:
		if l.StopReason == protocol.StopMaxIterations || l.StopReason == protocol.StopMaxMinutes {
			return "capped: " + stopReasonText[l.StopReason]
		}
		return "capped"
	case protocol.LoopRunning, protocol.LoopDone, protocol.LoopFailed, protocol.LoopStopped:
		return string(l.State)
	}
	return "unknown"
}

// loopIter is "iter/max" ("iter" alone when the cap is unknown).
func loopIter(iter, maxIter int) string {
	if maxIter > 0 {
		return fmt.Sprintf("%d/%d", iter, maxIter)
	}
	return fmt.Sprint(iter)
}

// loopChip is the text a loop session's row starts with: "⟳ 3/8 running".
// "" for an ordinary session, and while the loops feature is off. A loop the
// daemon no longer lists (finished long ago) shows its turn only.
func loopChip(m Model, s protocol.SessionSummary) string {
	if s.LoopID == "" || !m.featureOn(loopsFlag) {
		return ""
	}
	l, ok := loopByID(m, s.LoopID)
	if !ok {
		return loopGlyph() + " " + loopIter(s.LoopIter, 0)
	}
	return loopGlyph() + " " + loopIter(l.Iteration, l.MaxIterations) + " " + loopStateText(l)
}

// withLoopChip puts the loop chip in front of a row's text.
func withLoopChip(m Model, s protocol.SessionSummary, text string) string {
	chip := loopChip(m, s)
	if chip == "" {
		return text
	}
	if text == "" {
		return chip
	}
	return chip + " · " + text
}

// loopStyle is the loop state's color: the session-state tokens, so a theme
// restyles it.
func loopStyle(l protocol.LoopSummary) lipgloss.Style {
	switch l.State {
	case protocol.LoopRunning:
		return styleWorking
	case protocol.LoopPaused, protocol.LoopCapped:
		return styleNeeds
	case protocol.LoopDone:
		return styleDone
	case protocol.LoopFailed:
		return styleStateFailed
	case protocol.LoopStopped:
		return styleDim
	}
	return styleDim
}

// loopTarget resolves the loop a key or command acts on: the loop of the
// session named by args[0], else of the selected row. It sets m.Err and
// reports false when there is none.
func loopTarget(m *Model, verb string, args []string) (protocol.LoopSummary, bool) {
	id, ok := commandTarget(m, verb, args)
	if !ok {
		return protocol.LoopSummary{}, false
	}
	s, _ := findSession(m.Sessions, id)
	if s.LoopID == "" {
		m.Err = verb + ": " + displayName(s) + " is not a loop session"
		return protocol.LoopSummary{}, false
	}
	l, ok := loopByID(*m, s.LoopID)
	if !ok {
		m.Err = verb + ": the daemon no longer lists the loop of " + displayName(s)
		return protocol.LoopSummary{}, false
	}
	return l, true
}

// loopAct runs op (pause, resume or stop) on l. A loop that already ended
// only gets a notice; stop opens the confirmation first (confirmed is false
// then). back is the focus to return to after the confirmation.
func loopAct(m Model, op string, l protocol.LoopSummary, confirmed bool, back Focus) (Model, tea.Cmd) {
	if l.State.Terminal() {
		m.Notice = "loop " + l.Name + " already ended (" + loopStateText(l) + ")"
		return m, nil
	}
	switch {
	case op == "pause" && l.State == protocol.LoopPaused:
		m.Notice = "loop " + l.Name + " is already paused"
		return m, nil
	case op == "resume" && l.State != protocol.LoopPaused:
		m.Notice = "loop " + l.Name + " is not paused"
		return m, nil
	case op == "stop" && !confirmed:
		m.PendingLoopStop, m.loopStopBack = l.ID, back
		m.Focus = FocusConfirmLoopStop
		if m.Audio != nil {
			m.Audio.Play(audio.EventOpen)
		}
		return m, nil
	}
	ctl := looperOf(m)
	if ctl == nil {
		m.Err = "loop " + op + ": not connected to the daemon"
		return m, nil
	}
	slog.Info("tui: loop intent", "op", op, "loop", l.ID)
	return m, loopActionCmd(ctl, op, l.ID)
}

// loopActionCmd sends the intent; the reply returns as a loopActionMsg.
func loopActionCmd(ctl loopController, op, id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loopCallTimeout)
		defer cancel()
		var (
			l   protocol.LoopSummary
			err error
		)
		switch op {
		case "pause":
			l, err = ctl.PauseLoop(ctx, id)
		case "resume":
			l, err = ctl.ResumeLoop(ctx, id)
		default:
			l, err = ctl.StopLoop(ctx, id)
		}
		return loopActionMsg{Op: op, Loop: l, Err: err}
	}
}

// handleLoopAction applies the daemon's answer: the loop's new state, or the
// refusal on the status line.
func handleLoopAction(m Model, msg loopActionMsg) Model {
	if msg.Err != nil {
		slog.Warn("tui: loop intent failed", "op", msg.Op, "err", msg.Err)
		m.Err = "loop " + msg.Op + ": " + msg.Err.Error()
		return m
	}
	m = upsertLoop(m, msg.Loop)
	m.Err = ""
	m.Notice = "loop " + msg.Loop.Name + ": " + loopStateText(msg.Loop)
	return m
}

// loopKey is the p and x board keys: the selected row's loop. A row that is
// not a loop session says so on the status line.
func loopKey(m Model, op string) (Model, tea.Cmd) {
	if refuseOff(&m, loopsFlag) {
		return m, nil
	}
	m.Err = ""
	l, ok := loopTarget(&m, op, nil)
	if !ok {
		return m, nil
	}
	if op == "pause" && l.State == protocol.LoopPaused {
		op = "resume" // p toggles
	}
	return loopAct(m, op, l, false, FocusBoard)
}

// runLoopCmd is `:loop pause|resume|stop [sel]`.
func runLoopCmd(m Model, c cmdCall) (Model, tea.Cmd) {
	const usage = "loop: use :loop pause|resume|stop [session]"
	if len(c.Args) == 0 || len(c.Args) > 2 {
		m.Err = usage
		return m, nil
	}
	op := c.Args[0]
	if op != "pause" && op != "resume" && op != "stop" {
		m.Err = usage
		return m, nil
	}
	l, ok := loopTarget(&m, "loop "+op, c.Args[1:])
	if !ok {
		return m, nil
	}
	m.Err = ""
	return loopAct(m, op, l, false, FocusBoard)
}

// runLoops is `:loops`: open the overlay, or close it when it is open.
func runLoops(m Model, _ cmdCall) (Model, tea.Cmd) {
	if m.Focus == FocusLoops {
		m.Focus = FocusBoard
		return m, nil
	}
	return openLoops(m), nil
}

func openLoops(m Model) Model {
	m.Focus = FocusLoops
	m.LoopsSel = 0
	m.Err = ""
	return m
}

// updateConfirmLoopStopKey answers the stop confirmation: y stops, any other
// key leaves the loop running.
func updateConfirmLoopStopKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	id, back := m.PendingLoopStop, m.loopStopBack
	m.PendingLoopStop = ""
	m.Focus = back
	if k.String() != "y" && k.String() != "Y" {
		return m, nil
	}
	l, ok := loopByID(m, id)
	if !ok {
		m.Err = "loop stop: the loop is gone"
		return m, nil
	}
	return loopAct(m, "stop", l, true, back)
}

// renderLoopStopConfirm is the bottom bar of the stop confirmation.
func renderLoopStopConfirm(m Model, width int) string {
	name := m.PendingLoopStop
	iter := ""
	if l, ok := loopByID(m, m.PendingLoopStop); ok {
		name, iter = l.Name, " ("+loopIter(l.Iteration, l.MaxIterations)+")"
	}
	line := styleArrowYellow.Render("?") + " " +
		stylePrimary.Render("stop loop ") +
		styleSlug.Render(protocol.FlattenDisplayText(name)) +
		stylePrimary.Render(iter+"? it ends for good; the agent keeps running — ") +
		styleDim.Render("y / N")
	return padLine(" "+line, width)
}

// loopRows is the loops in overlay order: live ones (running, paused) first,
// then finished ones, newest first in each.
func loopRows(m Model) []protocol.LoopSummary {
	rows := slices.Clone(m.Loops)
	slices.SortStableFunc(rows, func(a, b protocol.LoopSummary) int {
		if at, bt := a.State.Terminal(), b.State.Terminal(); at != bt {
			if at {
				return 1
			}
			return -1
		}
		return b.StartedAt.Compare(a.StartedAt)
	})
	return rows
}

// updateLoopsKey handles keys while the overlay is open.
func updateLoopsKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	rows := loopRows(m)
	sel := min(m.LoopsSel, max(len(rows)-1, 0))
	switch k.String() {
	case "esc", "q", "L":
		m.Focus = FocusBoard
	case "j", "down":
		m.LoopsSel = min(sel+1, max(len(rows)-1, 0))
	case "k", "up":
		m.LoopsSel = max(sel-1, 0)
	case "g":
		m.LoopsSel = 0
	case "G":
		m.LoopsSel = max(len(rows)-1, 0)
	case "p", "x":
		if len(rows) == 0 {
			return m, nil
		}
		op := "pause"
		if k.String() == "x" {
			op = "stop"
		} else if rows[sel].State == protocol.LoopPaused {
			op = "resume"
		}
		m.Err = ""
		return loopAct(m, op, rows[sel], false, FocusLoops)
	case "enter":
		// Jump to the loop's session on the board.
		if len(rows) == 0 || rows[sel].SessionID == "" {
			return m, nil
		}
		if sessionIndex(m.Sessions, rows[sel].SessionID) < 0 {
			m.Err = "loops: the session of " + rows[sel].Name + " is not on the board"
			return m, nil
		}
		m.Focus = FocusBoard
		m.SelectedID = rows[sel].SessionID
		return ensureVisible(m), nil
	}
	return m, nil
}

// loopsInner is the overlay's text width on a w-column screen.
func loopsInner(w int) int { return max(min(w-12, 110), 40) }

// loopElapsed is the running time of l now: what the daemon last wrote plus
// the time since, while it runs.
func loopElapsed(m Model, l protocol.LoopSummary) time.Duration {
	d := time.Duration(l.ElapsedSeconds) * time.Second
	if l.State == protocol.LoopRunning && !l.UpdatedAt.IsZero() {
		d += max(m.now().Sub(l.UpdatedAt), 0)
	}
	return d
}

// loopCheckText is the last check's result.
func loopCheckText(l protocol.LoopSummary) string {
	switch {
	case l.LastCheckExit == nil:
		return "no check yet"
	case *l.LastCheckExit == 0:
		return "check passed"
	}
	return fmt.Sprintf("check exit %d", *l.LastCheckExit)
}

// renderLoopsOverlay renders the overlay content (the panel comes from
// centerPanel). Daemon-written names and notices go through
// FlattenDisplayText; the state words are rex's own.
func renderLoopsOverlay(m Model) string {
	inner := loopsInner(m.Width)
	rows := loopRows(m)
	sel := min(m.LoopsSel, max(len(rows)-1, 0))

	lines := []string{styleSectionTitle.Render("Loops"), ""}
	if len(rows) == 0 {
		lines = append(lines, styleDim.Render("No loops. Start one with `rex loop start <name>`."))
	}
	nameW, stateW := 0, 0
	for _, l := range rows {
		nameW = max(nameW, ansi.StringWidth(truncate(protocol.FlattenDisplayText(l.Name), overviewNameMax)))
		stateW = max(stateW, ansi.StringWidth(loopStateText(l)))
	}
	for i, l := range rows {
		pointer := " "
		if i == sel {
			pointer = styleBold.Render("›")
		}
		name := truncate(protocol.FlattenDisplayText(l.Name), overviewNameMax)
		name += strings.Repeat(" ", nameW-ansi.StringWidth(name))
		state := loopStateText(l)
		state = loopStyle(l).Render(state) + strings.Repeat(" ", stateW-ansi.StringWidth(state))
		elapsed := shortDuration(loopElapsed(m, l))
		if l.MaxMinutes > 0 {
			elapsed += "/" + fmt.Sprintf("%dm", l.MaxMinutes)
		}
		line := fmt.Sprintf("%s %s %s  %s  %s  %s  %s",
			pointer, loopGlyph(), name, state, loopIter(l.Iteration, l.MaxIterations), elapsed, loopCheckText(l))
		lines = append(lines, ansi.Truncate(line, inner, "…"))
		if l.Notice != "" && i == sel {
			lines = append(lines, "    "+styleMuted.Render(truncateWords(protocol.FlattenDisplayText(l.Notice), inner-4)))
		}
	}
	lines = append(lines, "", styleMuted.Render("  j/k select · p pause/resume · x stop · enter show session · esc close"))
	return strings.Join(lines, "\n")
}
