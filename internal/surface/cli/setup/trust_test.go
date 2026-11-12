package setup

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// trustHome isolates HOME, makes a project folder in it and returns both.
func trustHome(t *testing.T) (home, project string) {
	t.Helper()
	home = t.TempDir()
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	project = filepath.Join(home, "dev", "proj")
	require.NoError(t, os.MkdirAll(filepath.Join(project, ".git"), 0o700))
	return home, project
}

func storedTrustDirs(t *testing.T) string {
	t.Helper()
	store, _, err := settings.LoadDefault()
	require.NoError(t, err)
	return store.String("auto_trust_dirs")
}

type trustResultData struct {
	Action  string `json:"action"`
	Dir     string `json:"dir"`
	Changed bool   `json:"changed"`
	Value   string `json:"value"`
}

// trustAtTerminal runs rex trust as a person at a terminal (text, stdin and
// stdout terminals): the only way it adds a folder.
func trustAtTerminal(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, errw bytes.Buffer
	env := core.Env{Getenv: func(string) string { return "" }, Width: 80, Scheme: "default", GOOS: "linux",
		InTTY: true, OutTTY: true, ErrTTY: true}
	p, err := core.NewPrinter(core.FormatFlags{NoColor: true}, env, &out, &errw)
	require.NoError(t, err)
	core.SetOut(p)
	t.Cleanup(func() { core.SetOut(nil) })
	err = RunTrust(args)
	return out.String(), err
}

// trustAdd adds args at a terminal and requires success.
func trustAddOK(t *testing.T, args ...string) string {
	t.Helper()
	out, err := trustAtTerminal(t, args...)
	require.NoError(t, err)
	return out
}

func runTrustJSON(t *testing.T, args ...string) trustResultData {
	t.Helper()
	out, err := themeRun(t, "json", func() error { return RunTrust(args) })
	require.NoError(t, err)
	var env struct {
		Data trustResultData `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &env), out)
	return env.Data
}

func TestRunTrust_AddListRemove(t *testing.T) {
	home, project := trustHome(t)
	other := filepath.Join(home, "dev", "other")
	require.NoError(t, os.MkdirAll(other, 0o700))

	out := trustAddOK(t, project)
	require.Contains(t, out, "trusted "+project, "the exact folder is reported")
	require.Equal(t, project, storedTrustDirs(t), "stored as typed")

	// Adding again, or a folder inside it, changes nothing.
	out = trustAddOK(t, filepath.Join(project, "sub"))
	require.Contains(t, out, "already trusted")
	require.Equal(t, project, storedTrustDirs(t))

	trustAddOK(t, "~/dev/other")
	require.Equal(t, project+string(filepath.ListSeparator)+"~/dev/other", storedTrustDirs(t))

	text, err := themeRun(t, "text", func() error { return RunTrust([]string{"-ls"}) })
	require.NoError(t, err)
	require.Contains(t, text, project)
	require.Contains(t, text, other)

	agent, err := themeRun(t, "agent", func() error { return RunTrust([]string{"-ls"}) })
	require.NoError(t, err)
	require.Contains(t, agent, "trust_dir")

	_, err = themeRun(t, "text", func() error { return RunTrust([]string{"-rm", "~/dev/proj"}) })
	require.NoError(t, err)
	require.Equal(t, "~/dev/other", storedTrustDirs(t))

	// Removing a folder that is not listed is a no-op, not an error.
	d := runTrustJSON(t, "-rm", project)
	require.False(t, d.Changed)
	require.Equal(t, "~/dev/other", storedTrustDirs(t))
}

func TestRunTrust_DefaultIsSuggestionForCwd(t *testing.T) {
	_, project := trustHome(t)
	sub := filepath.Join(project, "pkg", "x")
	require.NoError(t, os.MkdirAll(sub, 0o700))
	t.Chdir(sub)

	out := trustAddOK(t)
	require.Contains(t, out, "trusted "+project, "the repo root, never its parent")
	require.Equal(t, "~/dev/proj", storedTrustDirs(t))
}

func TestRunTrust_DefaultInHomeIsRefused(t *testing.T) {
	home, _ := trustHome(t)
	t.Chdir(home)
	_, err := themeRun(t, "text", func() error { return RunTrust(nil) })
	requireExit(t, err, core.ExitInvalidArgs)
	require.Empty(t, storedTrustDirs(t))
}

func TestRunTrust_Refusals(t *testing.T) {
	home, project := trustHome(t)
	for _, tc := range []struct{ name, arg, want string }{
		{"dotdot", project + "/../x", ".."},
		{"home", home, "home folder"},
		{"ancestor", filepath.Dir(home), "holds your home"},
		{"relative", "dev/proj", "absolute"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := themeRun(t, "text", func() error { return RunTrust([]string{tc.arg}) })
			requireExit(t, err, core.ExitInvalidArgs)
			require.ErrorContains(t, err, tc.want)
			require.Empty(t, storedTrustDirs(t))
		})
	}
}

func TestRunTrust_BadArgs(t *testing.T) {
	_, project := trustHome(t)
	for _, args := range [][]string{
		{"a", "b"},
		{"-ls", project},
		{"-ls", "-rm", project},
		{"-rm"},
		{"-rm", project, project},
		{"-bogus"},
	} {
		_, err := themeRun(t, "text", func() error { return RunTrust(args) })
		requireExit(t, err, core.ExitInvalidArgs)
	}
	cfg, err := paths.ConfigFile()
	require.NoError(t, err)
	_, err = os.Stat(cfg)
	require.True(t, os.IsNotExist(err), "bad args write nothing")
}

// rex doctor says `rex trust -rm <entry>` for an entry the daemon ignores:
// that command must work.
func TestRunTrust_RemoveIgnoredEntry(t *testing.T) {
	_, project := trustHome(t)
	cfg, err := paths.ConfigFile()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	sep := string(filepath.ListSeparator)
	require.NoError(t, os.WriteFile(cfg, []byte("auto_trust_dirs: \""+project+sep+"~\"\n"), 0o600))

	d := runTrustJSON(t, "-rm", "~")
	require.True(t, d.Changed)
	require.Equal(t, project, storedTrustDirs(t))
}

// Removing a folder that a listed parent still covers must say so: "not in
// the list" alone reads as "no longer trusted".
func TestRunTrust_RemoveReportsCoveringEntry(t *testing.T) {
	home, project := trustHome(t)
	trustAddOK(t, filepath.Join(home, "dev"))
	runTrustJSON(t, "-rm", project)

	out, err := themeRun(t, "json", func() error { return RunTrust([]string{"-rm", project}) })
	require.NoError(t, err)
	require.Contains(t, out, `"covered_by":"`+filepath.Join(home, "dev")+`"`, out)

	text, err := themeRun(t, "text", func() error { return RunTrust([]string{"-rm", project}) })
	require.NoError(t, err)
	require.Contains(t, text, "still trusted")
}

// Adding a trusted folder is for the person at the terminal, as rex ws
// trust is: agent mode, a data format, or stdin or stdout not a terminal
// exit 6 and write nothing, and no flag skips it. Removing and listing
// stay scriptable (they never grant trust).
func TestRunTrust_AddNeedsATerminal(t *testing.T) {
	_, project := trustHome(t)
	for _, format := range []string{"text", "json", "agent", "txt"} {
		_, err := themeRun(t, format, func() error { return RunTrust([]string{project}) })
		requireExit(t, err, core.ExitOperationRefused)
		require.ErrorContains(t, err, "terminal")
		require.Empty(t, storedTrustDirs(t), format)
	}
	for _, tty := range []core.Env{{InTTY: true}, {OutTTY: true, ErrTTY: true}} {
		tty.Getenv, tty.Width, tty.GOOS = func(string) string { return "" }, 80, "linux"
		var out bytes.Buffer
		p, err := core.NewPrinter(core.FormatFlags{NoColor: true}, tty, &out, &out)
		require.NoError(t, err)
		core.SetOut(p)
		requireExit(t, RunTrust([]string{project}), core.ExitOperationRefused)
		core.SetOut(nil)
		require.Empty(t, storedTrustDirs(t))
	}
	for _, args := range [][]string{{"-o", "json", project}, {"--agent", project}, {"-o", "text", "-o", "agent", project}} {
		_, err := trustAtTerminal(t, args...)
		requireExit(t, err, core.ExitOperationRefused)
		require.Empty(t, storedTrustDirs(t), args)
	}
	trustAddOK(t, project)
	require.Equal(t, project, storedTrustDirs(t))

	// -ls and -rm work without a terminal.
	out, err := themeRun(t, "agent", func() error { return RunTrust([]string{"-ls"}) })
	require.NoError(t, err)
	require.Contains(t, out, project)
	d := runTrustJSON(t, "-rm", project)
	require.True(t, d.Changed)
	require.Empty(t, storedTrustDirs(t))
}
