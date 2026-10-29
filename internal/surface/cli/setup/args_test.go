package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// A stray positional is rejected before anything runs. Before, flags after
// it were dropped: `rex uninstall x --dry-run` lost --dry-run and went on to
// a real uninstall. The safe flags come first here so a regression still
// cannot remove anything.
func TestSetupCommandArity(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	cases := []struct {
		name string
		run  func([]string) error
		args []string
	}{
		{"uninstall", RunUninstall, []string{"-dry-run", "-binaries=false", "x", "-yes"}},
		{"update", RunUpdate, []string{"-check", "x", "-yes"}},
		{"doctor", RunDoctor, []string{"-json", "x"}},
		{"setup", RunSetup, []string{"-preview", "x"}},
		{"config list", RunConfig, []string{"list", "x", "-json"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.run(c.args)
			var ee core.ExitError
			require.ErrorAs(t, err, &ee)
			require.Equal(t, core.ExitInvalidArgs, ee.Code, ee.Msg)
			require.Contains(t, ee.Msg, `unexpected argument "x"`)
		})
	}
}

// -y is a real alias of -yes, wherever it appears: with an existing config
// it hits the same overwrite guard instead of starting the wizard.
func TestRunSetup_YAlias(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	cfg := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfg, []byte("theme: dark\n"), 0o644))

	err := RunSetup([]string{"-config", cfg, "-y"})
	var ee core.ExitError
	require.ErrorAs(t, err, &ee)
	require.Equal(t, core.ExitInvalidArgs, ee.Code)
	require.Contains(t, ee.Msg, "--force")
}
