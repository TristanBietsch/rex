package session

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// undoTimeout bounds one Undo or Redo: the daemon takes a safety capture
// and restores the folder before it answers.
const undoTimeout = 2 * time.Minute

// shellEffectsNote is shown in every undo and redo confirmation: rex only
// restores the files in the folder.
const shellEffectsNote = "shell side effects (installs, databases, files outside the folder) are not undone"

// undoData is the `-o json` payload of rex undo and rex redo.
type undoData struct {
	Session  string   `json:"session"`
	Op       string   `json:"op"`
	Turns    int      `json:"turns,omitempty"`
	Restored []string `json:"restored"`
	Deleted  []string `json:"deleted"`
	Safety   string   `json:"safety"`
	Warnings []string `json:"warnings,omitempty"`
}

// RunUndo restores a session's folder to how it was before the agent's last
// turn (-turns N: the last N turns), from the turn snapshots. It asks first
// (the question lists the file counts); -yes skips it and is required when
// stdin is not a terminal. -force goes ahead although another live session
// works in the same folder. rex redo reverses it.
func RunUndo(args []string) error {
	return runUndoRedo("undo", args)
}

// RunRedo brings back the state the latest rex undo replaced.
func RunRedo(args []string) error {
	return runUndoRedo("redo", args)
}

func runUndoRedo(op string, args []string) error {
	fs := flag.NewFlagSet(op, flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	out := core.AddOutputFlags(fs, core.AllFormats)
	turns := 1
	if op == "undo" {
		fs.IntVar(&turns, "turns", 1, "how many agent turns to go back")
	}
	force := fs.Bool("force", false, "go ahead although another live session works in the same folder")
	var yes bool
	fs.BoolVar(&yes, "yes", false, "do not ask (required when stdin is not a terminal)")
	fs.BoolVar(&yes, "y", false, "shorthand for -yes")
	rest, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return core.Errorf(core.ExitInvalidArgs, "rex "+op+" -h", "%s: exactly one selector required", op)
	}
	if turns < 1 || turns > 1000 {
		return core.Errorf(core.ExitInvalidArgs, "", "%s: -turns must be 1 to 1000", op)
	}
	if err := core.RequireFeature("turn_snapshots"); err != nil {
		return err
	}
	c, err := socket.Dial()
	if err != nil {
		return err
	}
	defer c.Close()
	sess, err := core.ResolveSelector(c, rest[0])
	if err != nil {
		return err
	}

	// Ask the daemon what it would do before anything changes: the refusals
	// (working, shared folder, no repo, nothing to undo) come first, and the
	// question can say how many files are touched.
	plan, err := undoCall(c, op, protocol.Undo{SessionID: sess.ID, Turns: turns, Force: *force, DryRun: true}, *force)
	if err != nil {
		return err
	}
	if !yes {
		if !pruneIO.interactive() {
			return core.Errorf(core.ExitOperationRefused, "rex "+op+" -yes "+rest[0],
				"%s: stdin is not a terminal; pass -yes to restore files without asking", op)
		}
		fmt.Fprintf(pruneIO.errOut, "%s %s: restore %d file(s), delete %d file(s)\n%s\nproceed? [y/N] ",
			op, undoSubject(sess, op, turns), len(plan.Restored), len(plan.Deleted), shellEffectsNote)
		var ans string
		_, _ = fmt.Fscanln(pruneIO.in, &ans)
		if a := strings.ToLower(ans); a != "y" && a != "yes" {
			return core.Errorf(core.ExitOperationRefused, "", "%s: not confirmed, nothing changed", op)
		}
	}
	res, err := undoCall(c, op, protocol.Undo{SessionID: sess.ID, Turns: turns, Force: *force}, *force)
	if err != nil {
		return err
	}
	return core.Out().Emit(undoResult(op, sess, turns, res))
}

// undoCall sends one Undo or Redo and returns the daemon's UndoResult, with
// daemon refusals turned into exit-coded errors.
func undoCall(c *client.Client, op string, p protocol.Undo, force bool) (protocol.UndoResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), undoTimeout)
	defer cancel()
	var payload any = p
	intent := protocol.IntentUndo
	if op == "redo" {
		intent = protocol.IntentRedo
		payload = protocol.Redo{SessionID: p.SessionID, Force: p.Force, DryRun: p.DryRun}
	}
	env, err := c.Do(ctx, intent, payload)
	if err != nil {
		return protocol.UndoResult{}, undoFailure(op, err, force)
	}
	var res protocol.UndoResult
	if env.Type != protocol.EventUndoResult {
		return res, core.Errorf(core.ExitGeneric, "rex daemon restart", "%s: unexpected %s reply from the daemon", op, env.Type)
	}
	if err := json.Unmarshal(env.Data, &res); err != nil {
		return res, core.Errorf(core.ExitGeneric, "", "%s: %w", op, err)
	}
	return res, nil
}

func undoFailure(op string, err error, force bool) error {
	var ce *client.Error
	if !errors.As(err, &ce) {
		return core.Errorf(core.ExitGeneric, "", "%s: %w", op, err)
	}
	switch ce.Code {
	case protocol.ErrCodeUnknownSession:
		return core.Errorf(core.ExitSelectorNotFound, "rex ls", "%s: %s", op, ce.Message)
	case protocol.ErrCodeBadIntent:
		if strings.HasPrefix(ce.Message, "off:") {
			return core.Errorf(core.ExitInvalidArgs, "rex config set turn_snapshots true", "%s: %s", op, ce.Message)
		}
		return core.Errorf(core.ExitInvalidArgs, "", "%s: %s", op, ce.Error())
	case protocol.ErrCodeBadState:
		hint := ""
		if strings.Contains(ce.Message, "another live session") && !force {
			hint = "rex " + op + " -force <session>"
		}
		return core.Errorf(core.ExitOperationRefused, hint, "%s: %s", op, ce.Message)
	}
	return core.Errorf(core.ExitGeneric, "", "%s: %s", op, ce.Error())
}

func undoSubject(sess protocol.SessionSummary, op string, turns int) string {
	slug := protocol.FlattenDisplayText(sess.Slug)
	if op == "undo" {
		return fmt.Sprintf("%s (%d turn(s))", slug, turns)
	}
	return slug
}

// undoResult lays out what an undo or redo did.
func undoResult(op string, sess protocol.SessionSummary, turns int, res protocol.UndoResult) core.Result {
	data := undoData{Session: sess.ID, Op: op, Restored: res.Restored, Deleted: res.Deleted, Safety: res.Safety, Warnings: res.Warnings}
	if op == "undo" {
		data.Turns = turns
	}
	slug := protocol.FlattenDisplayText(sess.Slug)
	v := core.View{
		core.Line{Tone: core.ToneOK, Glyph: "✓", Text: fmt.Sprintf("%s %s: %d file(s) restored, %d deleted", op, slug, len(res.Restored), len(res.Deleted)),
			Txt: fmt.Sprintf("%s\t%s\t%d\t%d", op, sess.ShortID, len(res.Restored), len(res.Deleted))},
	}
	v = append(v, fileList("restored", res.Restored)...)
	v = append(v, fileList("deleted", res.Deleted)...)
	for _, w := range res.Warnings {
		v = append(v, core.Note{Tone: core.ToneWarn, Text: w})
	}
	v = append(v, core.Note{Tone: core.ToneDim, Text: shellEffectsNote})
	if op == "undo" {
		v = append(v,
			core.Note{Tone: core.ToneDim, Text: "the agent was not told; it still remembers that turn. Tell it, for example: \"I reverted your last change\""},
			core.Hint{Cmd: "rex redo " + sess.ShortID, Text: "bring the changes back"})
	}
	return core.Result{Cmd: op, Data: data, View: v}
}

// maxListedFiles bounds the files named per list in the human views.
const maxListedFiles = 20

func fileList(label string, files []string) core.View {
	if len(files) == 0 {
		return nil
	}
	v := core.View{core.Section{Title: label}}
	for i, f := range files {
		if i == maxListedFiles {
			v = append(v, core.Note{Tone: core.ToneDim, Text: fmt.Sprintf("… and %d more", len(files)-maxListedFiles)})
			break
		}
		v = append(v, core.Line{Text: f})
	}
	return v
}
