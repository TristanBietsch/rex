package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// :undo and :redo (R01-1-4): restore a session's folder from the daemon's
// turn snapshots. The daemon decides everything; the TUI asks it for a dry
// run, shows what would change, and only then sends the real intent.

// undoTimeout bounds one Undo or Redo call: a safety capture plus a restore.
const undoTimeout = 2 * time.Minute

// shellEffectsNote is shown in every undo and redo dialog: rex restores the
// files in the folder, nothing else.
const shellEffectsNote = "shell side effects (installs, databases, files outside the folder) are not undone"

// undoListed bounds the files named per list in the dialog.
const undoListed = 6

// undoPhase is where the dialog is.
type undoPhase int

const (
	undoPlanning undoPhase = iota // the dry run is in flight
	undoShared                    // another live session shares the folder: force or cancel
	undoConfirm                   // the plan is shown: y or n
	undoRunning                   // the real call is in flight
	undoDone                      // the result (and warnings) are shown
)

// UndoState is the :undo / :redo dialog; it is set while FocusUndo.
type UndoState struct {
	Op        string // "undo" or "redo"
	SessionID string
	Name      string // the session's display name
	Turns     int    // undo only
	Force     bool   // the user chose to go ahead although another session shares the folder
	Phase     undoPhase
	Shared    string // the daemon's refusal naming the other live sessions
	Plan      protocol.UndoResult
	Result    protocol.UndoResult
	Err       string // the call failed after the dialog opened
	seq       int
}

// undoDoer is the one daemon call the dialog makes. *client.Client satisfies
// it; tests swap a fake.
type undoDoer interface {
	Do(ctx context.Context, intentType string, payload any) (protocol.Envelope, error)
}

func undoDoerOf(m Model) undoDoer {
	if m.undoer != nil {
		return m.undoer
	}
	if m.Client == nil {
		return nil
	}
	return m.Client
}

// undoReplyMsg is the daemon's answer to a dry run (final false) or to the
// real call (final true).
type undoReplyMsg struct {
	seq   int
	final bool
	res   protocol.UndoResult
	err   error
}

func runUndo(m Model, c cmdCall) (Model, tea.Cmd) { return startUndo(m, "undo", c) }
func runRedo(m Model, c cmdCall) (Model, tea.Cmd) { return startUndo(m, "redo", c) }

// startUndo parses `:undo [sel] [-turns N]` / `:redo [sel]`, opens the dialog
// and asks the daemon for a dry run. The turn_snapshots flag is checked by
// executeCommand.
func startUndo(m Model, op string, c cmdCall) (Model, tea.Cmd) {
	turns := 1
	var sel []string
	for i := 0; i < len(c.Args); i++ {
		a := c.Args[i]
		if a != "-turns" && a != "--turns" {
			sel = append(sel, a)
			continue
		}
		n, err := 0, error(nil)
		if i+1 < len(c.Args) {
			n, err = strconv.Atoi(c.Args[i+1])
		}
		switch {
		case op == "redo":
			m.Err = "redo: no -turns (it brings back the last undo)"
			return m, nil
		case i+1 >= len(c.Args) || err != nil || n < 1 || n > 1000:
			m.Err = "undo: -turns must be 1 to 1000"
			return m, nil
		}
		turns = n
		i++
	}
	if len(sel) > 1 {
		m.Err = op + ": at most one session"
		return m, nil
	}
	id, ok := commandTarget(&m, op, sel)
	if !ok {
		return m, nil
	}
	sum, found := findSession(m.Sessions, id)
	if !found {
		m.Err = op + ": session is gone"
		return m, nil
	}
	if undoDoerOf(m) == nil {
		m.Err = op + ": not connected to the daemon"
		return m, nil
	}
	m.Err = ""
	m.Undo = &UndoState{Op: op, SessionID: id, Name: displayName(sum), Turns: turns, Phase: undoPlanning}
	m.Focus = FocusUndo
	return sendUndo(m, true)
}

// sendUndo sends the dry run (dry true) or the real call for the dialog's
// session, with the dialog's force choice.
func sendUndo(m Model, dry bool) (Model, tea.Cmd) {
	final := !dry
	st := *m.Undo
	d := undoDoerOf(m)
	m.undoSeq++
	st.seq = m.undoSeq
	if final {
		st.Phase = undoRunning
	} else {
		st.Phase = undoPlanning
	}
	m.Undo = &st
	seq, op, id, turns, force := st.seq, st.Op, st.SessionID, st.Turns, st.Force
	slog.Info("tui: undo call", "op", op, "session", id, "dry_run", dry, "force", force)
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), undoTimeout)
		defer cancel()
		res, err := callUndo(ctx, d, op, protocol.Undo{SessionID: id, Turns: turns, Force: force, DryRun: dry})
		return undoReplyMsg{seq: seq, final: final, res: res, err: err}
	}
}

// callUndo sends one Undo or Redo and decodes the UndoResult.
func callUndo(ctx context.Context, d undoDoer, op string, p protocol.Undo) (protocol.UndoResult, error) {
	intent := protocol.IntentUndo
	var payload any = p
	if op == "redo" {
		intent = protocol.IntentRedo
		payload = protocol.Redo{SessionID: p.SessionID, Force: p.Force, DryRun: p.DryRun}
	}
	env, err := d.Do(ctx, intent, payload)
	if err != nil {
		return protocol.UndoResult{}, err
	}
	if env.Type != protocol.EventUndoResult {
		return protocol.UndoResult{}, fmt.Errorf("unexpected %s reply from the daemon", env.Type)
	}
	var res protocol.UndoResult
	if err := json.Unmarshal(env.Data, &res); err != nil {
		return protocol.UndoResult{}, fmt.Errorf("decode reply: %w", err)
	}
	return res, nil
}

// undoSharedFolder reports whether err is the daemon's "another live session
// works in this folder" refusal, the one case a force option answers.
func undoSharedFolder(err error) (string, bool) {
	var ce *client.Error
	if errors.As(err, &ce) && ce.Code == protocol.ErrCodeBadState && strings.Contains(ce.Message, "another live session") {
		return protocol.FlattenDisplayText(ce.Message), true
	}
	return "", false
}

// undoErrText is a refusal or failure in the status line's words.
func undoErrText(op string, err error) string {
	var ce *client.Error
	if errors.As(err, &ce) {
		switch {
		case ce.Code == protocol.ErrCodeUnknownSession:
			return op + ": session is gone"
		case ce.Code == protocol.ErrCodeBadIntent && strings.HasPrefix(ce.Message, "off:"):
			return offMessage("turn_snapshots")
		}
	}
	return op + ": " + protocol.FlattenDisplayText(err.Error())
}

// applyUndoReply moves the dialog on with the daemon's answer. A reply for a
// closed dialog or a request the dialog moved past is dropped.
func applyUndoReply(m Model, msg undoReplyMsg) Model {
	if m.Undo == nil || msg.seq != m.Undo.seq {
		return m
	}
	st := *m.Undo
	m.Undo = &st
	switch {
	case msg.err != nil && !msg.final:
		if shared, ok := undoSharedFolder(msg.err); ok && !st.Force {
			st.Phase, st.Shared = undoShared, shared
			return m
		}
		return closeUndoWith(m, undoErrText(st.Op, msg.err))
	case msg.err != nil:
		// The restore itself failed (or the guard tripped at the last
		// moment): show it in the dialog, since the folder may be part way.
		st.Phase, st.Err = undoDone, undoErrText(st.Op, msg.err)
		return m
	case msg.final:
		st.Phase, st.Result = undoDone, msg.res
	default:
		st.Phase, st.Plan = undoConfirm, msg.res
	}
	return m
}

// closeUndoWith closes the dialog and leaves a refusal on the status line.
func closeUndoWith(m Model, err string) Model {
	m.Undo = nil
	m.Focus = FocusBoard
	m.Err = err
	return m
}

func closeUndo(m Model) Model {
	m.Undo = nil
	m.Focus = FocusBoard
	return m
}

// updateUndoKey handles keys while the dialog is open.
func updateUndoKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	st := *m.Undo
	switch st.Phase {
	case undoConfirm:
		switch k.String() {
		case "y", "Y", "enter":
			return sendUndo(m, false)
		case "n", "N", "esc", "q":
			return closeUndo(m), nil
		}
	case undoShared:
		switch k.String() {
		case "f", "F":
			st.Force = true
			m.Undo = &st
			return sendUndo(m, true)
		case "n", "N", "esc", "q":
			return closeUndo(m), nil
		}
	case undoDone:
		switch k.String() {
		case "esc", "enter", "q", "y", "n":
			return closeUndo(m), nil
		}
	case undoPlanning:
		if k.String() == "esc" { // the dry run changes nothing: let go of it
			return closeUndo(m), nil
		}
	case undoRunning:
		// The daemon finishes the restore whatever the client does; wait for
		// its answer so the result and warnings are not lost.
	}
	return m, nil
}

// ---- view ------------------------------------------------------------------

// undoInner is the dialog's text width on a w-column screen.
func undoInner(w int) int { return max(min(w-14, 72), 30) }

func plural(n int, one string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %ss", n, one)
}

// renderUndo renders the dialog content (the title is its first line, the
// border comes from centerPanel).
func renderUndo(m Model) string {
	st := m.Undo
	if st == nil {
		return ""
	}
	inner := undoInner(m.Width)
	title := strings.ToUpper(st.Op)
	var out []string
	wrap := func(style func(string) string, s string) {
		for _, l := range strings.Split(ansi.Wrap(s, inner, ""), "\n") {
			out = append(out, style(l))
		}
	}
	dim := func(s string) string { return styleDim.Render(s) }
	warn := func(s string) string { return styleArrowYellow.Render(s) }
	plain := func(s string) string { return stylePrimary.Render(s) }

	subject := st.Name
	if st.Op == "undo" {
		subject += " · " + plural(st.Turns, "turn")
	}
	switch st.Phase {
	case undoPlanning:
		out = append(out, title, styleSlug.Render(truncate(subject, inner)), "", dim("checking what would change…"), "", renderHints([][2]string{{"esc", "cancel"}}, inner))
		return strings.Join(out, "\n")
	case undoShared:
		out = append(out, title, styleSlug.Render(truncate(subject, inner)), "")
		wrap(warn, st.Shared)
		out = append(out, "", dim("Going ahead reverts the other session's files too."), "",
			renderHints([][2]string{{"f", "force"}, {"esc", "cancel"}}, inner))
		return strings.Join(out, "\n")
	case undoRunning:
		out = append(out, title, styleSlug.Render(truncate(subject, inner)), "", dim("restoring…"))
		return strings.Join(out, "\n")
	case undoDone:
		out = append(out, title+"  "+"done", styleSlug.Render(truncate(subject, inner)), "")
		if st.Err != "" {
			wrap(warn, st.Err)
			out = append(out, "", dim("The folder may be part way: :redo brings back what was there before."))
		} else {
			out = append(out, plain(fmt.Sprintf("%s restored, %d deleted", plural(len(st.Result.Restored), "file"), len(st.Result.Deleted))))
			for _, w := range st.Result.Warnings {
				wrap(warn, "! "+protocol.FlattenDisplayText(w))
			}
		}
		out = append(out, "")
		wrap(dim, shellEffectsNote)
		if st.Op == "undo" && st.Err == "" {
			wrap(dim, "The agent was not told; it still remembers that turn. Tell it, for example: \"I reverted your last change\".")
			out = append(out, dim(":redo brings the changes back."))
		}
		out = append(out, "", renderHints([][2]string{{"esc", "close"}}, inner))
		return strings.Join(out, "\n")
	default:
	}

	out = append(out, title, styleSlug.Render(truncate(subject, inner)), "",
		plain(fmt.Sprintf("restore %s, delete %s", plural(len(st.Plan.Restored), "file"), plural(len(st.Plan.Deleted), "file"))))
	out = append(out, undoFileList("restore", st.Plan.Restored, inner)...)
	out = append(out, undoFileList("delete", st.Plan.Deleted, inner)...)
	if st.Force {
		out = append(out, "")
		wrap(warn, "forced: the other live session in this folder is reverted too ("+undoOthers(st.Shared)+")")
	}
	out = append(out, "")
	wrap(dim, shellEffectsNote)
	out = append(out, "", renderHints([][2]string{{"y", st.Op}, {"esc", "cancel"}}, inner))
	return strings.Join(out, "\n")
}

// undoOthers pulls the session names out of the daemon's refusal text.
func undoOthers(msg string) string {
	i, j := strings.Index(msg, "("), strings.Index(msg, ")")
	if i < 0 || j < i {
		return "another session"
	}
	return msg[i+1 : j]
}

// undoFileList names up to undoListed files of one list.
func undoFileList(verb string, files []string, inner int) []string {
	var out []string
	for i, f := range files {
		if i == undoListed {
			out = append(out, styleDim.Render(fmt.Sprintf("  … and %d more", len(files)-undoListed)))
			break
		}
		out = append(out, styleDim.Render("  "+verb+" ")+stylePrimary.Render(truncate(f, max(inner-4-len(verb), 8))))
	}
	return out
}
