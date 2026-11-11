package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/testutil/golden"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

const resumeTestID = "7d4f3c8a-1234-4abc-89ab-cdefabcdef01"

const resumeCLITools = `tools:
  - id: echo
    resume:
      args: ["--resume", "{id}"]
      new_args: ["--session-id", "{id}"]
    models:
      - id: argv
        name: "Prints its argv"
        args: ['echo "ARGV: $0 $*"; sleep 0.3']
`

func emitResume(t *testing.T, format string, width int, color bool, r core.Result) string {
	t.Helper()
	p, out := lsPrinter(t, format, width, color)
	require.NoError(t, p.Emit(r))
	return out.String()
}

func resumeSample(state protocol.State, retire bool) protocol.SessionSummary {
	s := protocol.SessionSummary{ID: "7d4f0000", ShortID: "7d4f", Slug: "fix-auth-flow", State: state, ResumeID: resumeTestID}
	if retire {
		s.RetireAt = lsNow
	}
	return s
}

func TestResumeOutcome(t *testing.T) {
	for _, tc := range []struct {
		s    protocol.SessionSummary
		want string
	}{
		{resumeSample(protocol.StateDone, false), resumeResuming},
		{resumeSample(protocol.StateFailed, false), resumeResuming},
		{resumeSample(protocol.StateCrashed, true), resumeResuming},
		{resumeSample(protocol.StateWorking, true), resumeUnmarked},
		{resumeSample(protocol.StateNeedsInput, true), resumeUnmarked},
		{resumeSample(protocol.StateWorking, false), resumeRunning},
		{resumeSample(protocol.StateQueued, false), resumeRunning},
	} {
		require.Equal(t, tc.want, resumeOutcome(tc.s), "%s retire=%v", tc.s.State, !tc.s.RetireAt.IsZero())
	}
}

func TestResumeGolden(t *testing.T) {
	var secs []golden.Section
	for _, o := range []string{resumeResuming, resumeUnmarked, resumeRunning} {
		sess := resumeSample(protocol.StateDone, false)
		for _, f := range []string{"text", "txt", "md", "json", "agent"} {
			p, _ := lsPrinter(t, f, 80, false)
			secs = append(secs, golden.Section{Name: o + "." + f, Body: emitResume(t, f, 80, false, core.Result{
				Cmd: "resume", Data: resumeResult{ID: sess.ID, ShortID: sess.ShortID, Slug: sess.Slug, Outcome: o},
				View: resumeView(sess, o, p.Glyphs)})})
		}
	}
	secs = append(secs, golden.Section{Name: "resuming.text.color", Body: emitResume(t, "text", 80, true, core.Result{
		Cmd: "resume", View: resumeView(resumeSample(protocol.StateDone, false), resumeResuming, core.GlyphsFor(false, ""))})})
	sess := resumeSample(protocol.StateDone, false)
	for _, f := range []string{"text", "txt", "md", "json", "agent"} {
		secs = append(secs, golden.Section{Name: "print." + f, Body: emitResume(t, f, 80, false, core.Result{
			Cmd: "resume", Data: resumeIDResult{ID: sess.ID, ResumeID: sess.ResumeID},
			View: core.ActionView(core.Line{Text: sess.ResumeID, Txt: sess.ResumeID},
				core.Field{Key: "id", Val: sess.ShortID}, core.Field{Key: "resume_id", Val: sess.ResumeID}),
		})})
	}
	golden.Check(t, "testdata/golden/resume.txtar", secs...)
}

func TestResumeErrorMapping(t *testing.T) {
	s := resumeSample(protocol.StateDone, false)
	code := func(err error) int {
		var ec core.ExitCoder
		require.True(t, errors.As(err, &ec), "%v", err)
		return ec.ExitCode()
	}
	e := func(c, m string) error { return &client.Error{Intent: protocol.IntentResume, Code: c, Message: m} }

	off := resumeError(e(protocol.ErrCodeBadIntent, "resume is off"), s)
	require.Equal(t, core.ExitInvalidArgs, code(off))
	require.Equal(t, core.OffMessage("resume"), off.Error())

	old := resumeError(e(protocol.ErrCodeBadIntent, "intent not implemented"), s)
	require.Contains(t, old.Error(), "older than rex")
	require.Equal(t, "rex daemon restart", core.Describe(old).Hint)
	require.Contains(t, resumeError(client.ErrNoAcks, s).Error(), "older than rex")

	require.Equal(t, core.ExitSelectorNotFound, code(resumeError(e(protocol.ErrCodeUnknownSession, "session not found"), s)))
	nr := resumeError(e(protocol.ErrCodeBadState, "session exited and echo cannot be resumed"), s)
	require.Equal(t, core.ExitGeneric, code(nr))
	require.Contains(t, nr.Error(), "cannot be resumed")
	require.Equal(t, core.ExitGeneric, code(resumeError(context.DeadlineExceeded, s)))
}

func TestRunResumeArgs(t *testing.T) {
	require.Equal(t, core.ExitInvalidArgs, exitOf(t, RunResume(nil)))
	require.Equal(t, core.ExitInvalidArgs, exitOf(t, RunResume([]string{"a", "b"})))
	require.Equal(t, core.ExitInvalidArgs, exitOf(t, RunResume([]string{"-bogus", "a"})))
}

func exitOf(t *testing.T, err error) int {
	t.Helper()
	var ec core.ExitCoder
	require.True(t, errors.As(err, &ec), "%v", err)
	return ec.ExitCode()
}

// rex resume against a real daemon: an exited session is respawned, -print
// shows its id, and the resume switch off refuses both with exit 5.
func TestRunResume_RealDaemon(t *testing.T) {
	dir := t.TempDir()
	d := testutil.StartDaemonWithTools(t, resumeCLITools)
	t.Cleanup(d.Close)

	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	defer c.Close()
	_, err = c.Hello("resume-test")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	env, err := c.Do(ctx, protocol.IntentNewSession, protocol.NewSession{
		ToolID: "echo", ModelID: "argv", Slug: "finished", CWD: dir,
	})
	require.NoError(t, err)
	var ack protocol.Ack
	require.NoError(t, json.Unmarshal(env.Data, &ack))
	state := func() protocol.SessionSummary {
		sc, err := client.Dial(d.Socket)
		require.NoError(t, err)
		defer sc.Close()
		snap, err := sc.Hello("resume-poll")
		require.NoError(t, err)
		for _, s := range snap.Sessions {
			if s.ID == ack.SessionID {
				return s
			}
		}
		return protocol.SessionSummary{}
	}
	require.Eventually(t, func() bool { return state().State == protocol.StateDone }, 15*time.Second, 100*time.Millisecond)
	id := state().ResumeID
	require.True(t, protocol.ValidResumeID(id), "the daemon preassigned an id: %q", id)

	run := func(format string, args ...string) (string, error) {
		p, out := lsPrinter(t, format, 80, false)
		core.SetOut(p)
		t.Cleanup(func() { core.SetOut(nil) })
		err := RunResume(append([]string{"-socket", d.Socket}, args...))
		return out.String(), err
	}

	out, err := run("txt", "-print", "finished")
	require.NoError(t, err)
	require.Equal(t, id+"\n", out, "-print prints only the full id")
	out, err = run("agent", "-print", ack.SessionID[:4])
	require.NoError(t, err)
	require.Equal(t, "ok cmd=resume id="+state().ShortID+" resume_id="+id+"\n", out)

	out, err = run("txt", "finished")
	require.NoError(t, err)
	require.Equal(t, "finished: resuming\n", out)
	require.Eventually(t, func() bool { return state().State == protocol.StateDone }, 15*time.Second, 100*time.Millisecond)

	out, err = run("agent", "finished")
	require.NoError(t, err)
	require.Contains(t, out, "ok cmd=resume id="+state().ShortID+" slug=finished outcome=resuming")

	_, err = run("txt", "nope")
	require.Equal(t, core.ExitSelectorNotFound, exitOf(t, err))

	cfg, err := paths.ConfigFile()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("resume: false\n"), 0o600))
	t.Cleanup(func() { _ = os.Remove(cfg) })
	for _, args := range [][]string{{"finished"}, {"-print", "finished"}} {
		out, err = run("txt", args...)
		require.Equal(t, core.ExitInvalidArgs, exitOf(t, err), strings.Join(args, " "))
		require.Equal(t, core.OffMessage("resume"), err.Error())
		require.Empty(t, out)
	}
}

// -print on a session with no resume id is an error, not an empty line.
func TestRunResumePrintWithoutID(t *testing.T) {
	dir := t.TempDir()
	d := testutil.StartDaemon(t) // the builtin echo tool has no resume block
	t.Cleanup(d.Close)
	c, err := client.Dial(d.Socket)
	require.NoError(t, err)
	defer c.Close()
	_, err = c.Hello("resume-test")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = c.Do(ctx, protocol.IntentNewSession, protocol.NewSession{ToolID: "echo", ModelID: "short", Slug: "noid", CWD: dir})
	require.NoError(t, err)

	p, out := lsPrinter(t, "txt", 80, false)
	core.SetOut(p)
	t.Cleanup(func() { core.SetOut(nil) })
	err = RunResume([]string{"-socket", d.Socket, "-print", "noid"})
	require.Equal(t, core.ExitGeneric, exitOf(t, err))
	require.Contains(t, err.Error(), "has no resume id")
	require.Empty(t, out.String())
}
