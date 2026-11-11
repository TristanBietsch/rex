package session

import (
	"context"
	"errors"
	"flag"
	"time"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// resumeTimeout bounds how long `rex resume` waits for the daemon's ack. An
// exited session is respawned before the ack, so it gets longer than a
// plain unmark.
const resumeTimeout = 10 * time.Second

// Resume outcomes, from the session's state when the command ran.
const (
	resumeResuming = "resuming"
	resumeUnmarked = "unmarked"
	resumeRunning  = "running"
)

// resumeResult is the `-o json` payload of rex resume.
type resumeResult struct {
	ID      string `json:"id"`
	ShortID string `json:"short_id"`
	Slug    string `json:"slug"`
	// Outcome is "resuming" (an exited session was respawned), "unmarked"
	// (a running session lost its done mark) or "running" (nothing to do).
	Outcome string `json:"outcome"`
}

// resumeIDResult is the `-o json` payload of rex resume -print.
type resumeIDResult struct {
	ID       string `json:"id"`
	ResumeID string `json:"resume_id"`
}

// RunResume makes a session live and not marked done: an exited session is
// respawned in place with its agent's resume arguments, a running one loses
// its done mark. `-print` sends nothing and prints the agent's full resume
// id, for `claude --resume $(rex resume -print <sel>)`.
func RunResume(args []string) error {
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	socket := core.AddSocketFlag(fs)
	out := core.AddOutputFlags(fs, core.AllFormats)
	printID := fs.Bool("print", false, "print only the full resume id; send nothing")
	rest, err := out.Parse(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return core.NewExitError(core.ExitInvalidArgs, "resume: exactly one selector required")
	}
	if !featureOn("resume") {
		return core.OffError("resume")
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
	p := core.Out()

	if *printID {
		if !protocol.ValidResumeID(sess.ResumeID) {
			return core.Errorf(core.ExitGeneric, "", "resume: %s has no resume id", sess.Slug)
		}
		// A scripting primitive: `claude --resume "$(rex resume -print sel)"`
		// gets the bare id even where CLAUDECODE switched agent mode on by
		// itself. An explicit -o, --agent or REX_AGENT=1 still decides.
		if p.AgentAuto && !p.Flags.Explicit() {
			if tp, err := p.WithFlags(core.FormatFlags{Output: "txt"}); err == nil {
				p = tp
			}
		}
		return p.Emit(core.Result{
			Cmd:  "resume",
			Data: resumeIDResult{ID: sess.ID, ResumeID: sess.ResumeID},
			View: core.ActionView(core.Line{Text: sess.ResumeID, Txt: sess.ResumeID},
				core.Field{Key: "id", Val: sess.ShortID}, core.Field{Key: "resume_id", Val: sess.ResumeID}),
		})
	}

	outcome := resumeOutcome(sess)
	ctx, cancel := context.WithTimeout(context.Background(), resumeTimeout)
	defer cancel()
	if _, err := c.Do(ctx, protocol.IntentResume, protocol.Resume{SessionID: sess.ID}); err != nil {
		return resumeError(err, sess)
	}
	return p.Emit(core.Result{
		Cmd:  "resume",
		Data: resumeResult{ID: sess.ID, ShortID: sess.ShortID, Slug: sess.Slug, Outcome: outcome},
		View: resumeView(sess, outcome, p.Glyphs),
	})
}

// resumeOutcome is what the daemon will do, judged from the snapshot: a
// finished session is respawned, a marked one unmarked, anything else is
// already live.
func resumeOutcome(s protocol.SessionSummary) string {
	switch s.State {
	case protocol.StateDone, protocol.StateFailed, protocol.StateCrashed:
		return resumeResuming
	case protocol.StateQueued, protocol.StateWorking, protocol.StateNeedsInput:
	}
	if !s.RetireAt.IsZero() {
		return resumeUnmarked
	}
	return resumeRunning
}

func resumeView(s protocol.SessionSummary, outcome string, g core.Glyphs) core.View {
	text := map[string]string{
		resumeResuming: "resuming " + s.ShortID + " " + s.Slug,
		resumeUnmarked: "unmarked " + s.ShortID + " " + s.Slug,
		resumeRunning:  s.ShortID + " " + s.Slug + " is already running",
	}[outcome]
	txt := map[string]string{
		resumeResuming: s.Slug + ": resuming",
		resumeUnmarked: s.Slug + ": unmarked",
		resumeRunning:  s.Slug + ": already running",
	}[outcome]
	return core.ActionView(
		core.Line{Tone: core.ToneOK, Glyph: g.OK, Text: text, Txt: txt},
		core.Field{Key: "id", Val: s.ShortID},
		core.Field{Key: "slug", Val: s.Slug},
		core.Field{Key: "outcome", Val: outcome},
	)
}

// resumeError maps a daemon refusal to an exit code and message.
func resumeError(err error, s protocol.SessionSummary) error {
	var de *client.Error
	switch {
	case errors.Is(err, client.ErrNoAcks):
		return core.Errorf(core.ExitGeneric, "rex daemon restart", "resume: daemon is older than rex")
	case errors.Is(err, context.DeadlineExceeded):
		return core.Errorf(core.ExitGeneric, "rex ls", "resume: daemon did not confirm within %s; check rex ls", resumeTimeout)
	case !errors.As(err, &de):
		return core.Errorf(core.ExitGeneric, "", "resume: %w", err)
	case de.Code == protocol.ErrCodeBadIntent && de.Message == "intent not implemented":
		return core.Errorf(core.ExitGeneric, "rex daemon restart", "resume: daemon is older than rex")
	case de.Code == protocol.ErrCodeBadIntent && de.Message == "resume is off":
		return core.OffError("resume")
	case de.Code == protocol.ErrCodeBadIntent:
		return core.Errorf(core.ExitInvalidArgs, "", "resume: %s", de.Message)
	case de.Code == protocol.ErrCodeUnknownSession:
		return core.Errorf(core.ExitSelectorNotFound, "rex ls", "resume: %s is gone", s.Slug)
	default:
		return core.Errorf(core.ExitGeneric, "", "resume: %s", de.Message)
	}
}
