package session

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// With no text and a terminal on stdin, reply would block on ReadAll.
func TestRunReply_TTYNoText(t *testing.T) {
	withPruneIO(t, "", true)
	err := RunReply([]string{"-socket", "/nonexistent/rex.sock", "x"})
	ee := requireInvalidArgs(t, err)
	require.Contains(t, ee.Msg, "reply:")
	require.Contains(t, ee.Msg, "text")
}

func TestRunReply_NoTTYNoTextReachesTheSocket(t *testing.T) {
	withPruneIO(t, "", false)
	r, w, err := os.Pipe()
	require.NoError(t, err)
	require.NoError(t, w.Close())
	saved := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = saved; _ = r.Close() })
	err = RunReply([]string{"-socket", "/nonexistent/rex.sock", "x"})
	require.Equal(t, 4, exitCode(t, err), "stdin is read (empty), then the dial fails")
}

// The flags kept only so old scripts still parse say they do nothing.
func TestUsage_DeprecatedFlagsMarked(t *testing.T) {
	help := func(run func([]string) error) string {
		p, out := lsPrinter(t, "txt", 80, false)
		core.SetOut(p)
		t.Cleanup(func() { core.SetOut(nil) })
		require.ErrorIs(t, run([]string{"-h"}), core.ErrHelpShown)
		return out.String()
	}
	ls := help(RunLs)
	require.Regexp(t, `-short\s+\(deprecated, no effect\)`, ls)
	require.Regexp(t, `-show-archived\s+\(deprecated, no effect\)`, ls)
	require.Regexp(t, `-no-attach\s+\(deprecated, no effect\)`, help(RunNew))
}

// A daemon that takes the intent but never reports the session: a message
// that says what to check, not a raw deadline error.
func TestRunNew_SpawnConfirmTimeoutMessage(t *testing.T) {
	d := startPruneDaemon(t, true)
	err := RunNew([]string{"-socket", d.sock, "-tool", "echo", "-model", "short", "-wait", "150ms", "hello"})
	require.Equal(t, 1, exitCode(t, err))
	require.ErrorContains(t, err, "daemon did not confirm the spawn within 150ms")
	require.ErrorContains(t, err, "rex ls")
	require.NotContains(t, err.Error(), "deadline")
}

func TestRunNew_BadWait(t *testing.T) {
	for _, w := range []string{"0", "-1s"} {
		err := RunNew([]string{"-socket", "/nonexistent/rex.sock", "-wait", w, "hello"})
		ee := requireInvalidArgs(t, err)
		require.Contains(t, ee.Msg, "-wait")
	}
}
