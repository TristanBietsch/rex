package session

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// atFakeTTY installs a text Printer whose stdin and stdout are terminals
// (tty) or pipes, and removes it when the test ends.
func atFakeTTY(t *testing.T, tty bool) {
	t.Helper()
	env := core.Env{Getenv: func(string) string { return "" }, InTTY: tty, OutTTY: tty, ErrTTY: tty,
		Width: 80, Scheme: "default", GOOS: "linux"}
	p, err := core.NewPrinter(core.FormatFlags{NoColor: true}, env, io.Discard, io.Discard)
	require.NoError(t, err)
	core.SetOut(p)
	t.Cleanup(func() { core.SetOut(nil) })
}

// trustHome makes a throwaway HOME with a project folder in it.
func trustHome(t *testing.T) (home, project string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	project = filepath.Join(home, "proj")
	require.NoError(t, os.MkdirAll(project, 0o700))
	return home, project
}

func TestParseNewArgs_TrustRefusesHomeAndAncestors(t *testing.T) {
	atFakeTTY(t, true)
	home, project := trustHome(t)

	require.NoError(t, checkNewTrust(false, home), "no -trust, nothing to check")
	require.NoError(t, checkNewTrust(true, project))
	for _, bad := range []string{home, filepath.Dir(home), string(filepath.Separator)} {
		ee := requireInvalidArgs(t, checkNewTrust(true, bad))
		require.NotEmpty(t, ee.Error(), bad)
	}
}

// -trust skips the agent's trust question (and, with skip_permissions,
// its permission prompts), so only a person at a terminal may pass it: an
// agent running rex new from a tool call must not start one.
func TestRunNew_TrustNeedsTerminal(t *testing.T) {
	atFakeTTY(t, false)
	_, project := trustHome(t)

	require.NoError(t, checkNewTrust(false, project), "no -trust needs no terminal")

	err := RunNew([]string{"-socket", "/nonexistent/rex.sock", "-tool", "echo", "-model", "short",
		"-trust", "-cwd", project, "hello"})
	require.Equal(t, int(core.ExitOperationRefused), exitCode(t, err), "refused before the daemon is dialed")
	require.ErrorContains(t, err, "only at a terminal")
}

// A folder the daemon would refuse is reported as such (exit 5) before the
// terminal check, as rex trust does.
func TestRunNew_TrustBadFolderBeforeTerminal(t *testing.T) {
	atFakeTTY(t, false)
	home, _ := trustHome(t)
	ee := requireInvalidArgs(t, checkNewTrust(true, home))
	require.Contains(t, ee.Error(), "home folder")
}
