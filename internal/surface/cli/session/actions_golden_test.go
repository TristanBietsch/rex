package session

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/testutil/golden"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// actionRun runs fn with a Printer in format whose stdout and stderr are
// buffers, and returns what each got.
func actionRun(t *testing.T, format string, fn func() error) (stdout, stderr string, err error) {
	t.Helper()
	var out, errw bytes.Buffer
	env := core.Env{Getenv: func(string) string { return "" }, Width: 80, Scheme: "default", GOOS: "linux"}
	p, perr := core.NewPrinter(core.FormatFlags{Output: format}, env, &out, &errw)
	require.NoError(t, perr)
	core.SetOut(p)
	t.Cleanup(func() { core.SetOut(nil) })
	err = fn()
	return out.String(), errw.String(), err
}

func findSession(t *testing.T, sock, slug string) protocol.SessionSummary {
	t.Helper()
	c, err := client.Dial(sock)
	require.NoError(t, err)
	defer c.Close()
	snap, err := c.Hello("golden-poll")
	require.NoError(t, err)
	for _, s := range snap.Sessions {
		if s.Slug == slug {
			return s
		}
	}
	return protocol.SessionSummary{}
}

// Every session action against a real daemon, in every format, with the
// per-run ids and paths normalized. txt stays what each command always
// printed (silent for rm, reply, send, wait and complete).
func TestActionCommandsGolden(t *testing.T) {
	dir := t.TempDir()
	d := testutil.StartDaemon(t)
	t.Cleanup(d.Close)
	sock := []string{"-socket", d.Socket}

	stdin := filepath.Join(t.TempDir(), "stdin")
	require.NoError(t, os.WriteFile(stdin, []byte("raw bytes"), 0o600))
	savedStdin := os.Stdin
	t.Cleanup(func() { os.Stdin = savedStdin })

	var secs []golden.Section
	for _, f := range []string{"text", "txt", "md", "json", "agent"} {
		slug := "g-" + f
		norm := func(s string) string { return s }
		add := func(name string, out, errOut string, err error) {
			body := norm(out)
			if errOut != "" {
				body += "[stderr]\n" + norm(errOut)
			}
			if err != nil {
				body += "[error] " + err.Error() + "\n"
			}
			secs = append(secs, golden.Section{Name: name + "." + f, Body: body})
		}
		run := func(name string, args func([]string) error, a ...string) {
			t.Helper()
			out, errOut, err := actionRun(t, f, func() error { return args(append(append([]string{}, sock...), a...)) })
			if name == "new" {
				require.NoError(t, err)
				s := findSession(t, d.Socket, slug)
				r := strings.NewReplacer(s.ID, "FULLID", s.ShortID, "SID", dir, "DIR")
				norm = r.Replace
				out = norm(out)
			}
			add(name, out, errOut, err)
		}

		run("new", RunNew, "-tool", "echo", "-model", "prompt", "-slug", slug, "-cwd", dir)
		require.Eventually(t, func() bool { return findSession(t, d.Socket, slug).State == protocol.StateNeedsInput },
			15*time.Second, 50*time.Millisecond)
		run("rename", RunRename, slug, slug+"-b")
		slug += "-b"
		run("name.set", RunName, slug, "--", "Auth fix")
		run("name.show", RunName, slug)
		run("name.clear", RunName, "-clear", slug)
		run("done", RunDone, slug)
		run("done.again", RunDone, slug)
		run("undone", RunUndone, slug)
		run("undone.again", RunUndone, slug)
		run("archive", RunArchive, slug)
		run("reply", RunReply, slug, "--", "yes")
		in, err := os.Open(stdin)
		require.NoError(t, err)
		os.Stdin = in
		run("send", RunSend, slug)
		_ = in.Close()
		run("wait", RunWait, slug, "-until", "needs_input")
		run("rm.refused", RunRm, slug)
		run("rm", RunRm, "-force", slug)
		require.Eventually(t, func() bool { return findSession(t, d.Socket, slug).ID == "" }, 15*time.Second, 50*time.Millisecond)
	}
	golden.Check(t, "testdata/golden/actions.txtar", secs...)
}
