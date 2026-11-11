package setup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// setupHome points HOME at a fresh temp dir and returns it with a
// config.yaml path inside that does not exist yet.
func setupHome(t *testing.T) (home, cfg string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_RUNTIME_DIR", "")
	return home, filepath.Join(home, ".config", "rex", "config.yaml")
}

// -auto-trust refuses every value the daemon would ignore, before anything
// is written.
func TestRunSetup_AutoTrustRefused(t *testing.T) {
	home, cfg := setupHome(t)
	sep := string(filepath.ListSeparator)
	cases := map[string]string{
		"root":            filepath.VolumeName(home) + string(filepath.Separator),
		"home":            "~",
		"home slash":      "~/",
		"home absolute":   home,
		"relative":        "dev",
		"dot-dot":         "~/dev/../work",
		"separator split": filepath.Join(home, "work") + sep + "b",
		"only separators": sep + " " + sep,
	}
	for name, v := range cases {
		t.Run(name, func(t *testing.T) {
			err := RunSetup([]string{"-yes", "-no-shell", "-config", cfg, "-auto-trust", v})
			var ee core.ExitError
			require.True(t, errors.As(err, &ee), "%v", err)
			require.Equal(t, core.ExitInvalidArgs, ee.Code)
			require.Contains(t, ee.Msg, "-auto-trust")
			require.NoFileExists(t, cfg)
		})
	}
}

func TestRunSetup_AutoTrustWritten(t *testing.T) {
	home, cfg := setupHome(t)
	sep := string(filepath.ListSeparator)
	work := filepath.Join(home, "work")

	require.NoError(t, RunSetup([]string{"-yes", "-no-shell", "-config", cfg,
		"-auto-trust", " ~/dev " + sep + work}))
	st := settings.NewStore()
	require.NoError(t, st.Load(cfg))
	require.Equal(t, "~/dev"+sep+work, st.String("auto_trust_dirs"))

	// The preview shows the value and writes nothing.
	require.NoError(t, os.Remove(cfg))
	require.NoError(t, RunSetup([]string{"-preview", "-no-shell", "-config", cfg, "-auto-trust", "~/dev"}))
	require.NoFileExists(t, cfg)
}

// -tool, -model and -effort are checked against the registry: a wrong one
// exits 5 and lists the valid ids, with nothing written.
func TestRunSetup_SpawnDefaultsRefused(t *testing.T) {
	_, cfg := setupHome(t)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"-tool", "nope"}, `unknown tool "nope" (valid: claude`},
		{[]string{"-tool", "claude", "-model", "gpt-9"}, "valid: opus, sonnet, haiku"},
		{[]string{"-tool", "claude", "-model", "opus", "-effort", "turbo"}, "valid: low, medium, high, xhigh, max"},
	}
	for _, tc := range cases {
		for _, mode := range []string{"-yes", "-preview"} {
			args := append([]string{mode, "-no-shell", "-config", cfg}, tc.args...)
			err := RunSetup(args)
			var ee core.ExitError
			require.True(t, errors.As(err, &ee), "%v", err)
			require.Equal(t, core.ExitInvalidArgs, ee.Code)
			require.Contains(t, ee.Msg, tc.want)
			require.NoFileExists(t, cfg)
		}
	}
}

func TestRunSetup_SpawnDefaultsWritten(t *testing.T) {
	_, cfg := setupHome(t)
	require.NoError(t, RunSetup([]string{"-yes", "-no-shell", "-config", cfg,
		"-tool", "antigravity", "-model", "claude-opus-4-6", "-effort", "high"}))
	st := settings.NewStore()
	require.NoError(t, st.Load(cfg))
	require.Equal(t, "antigravity", st.String("default_spawn_tool"))
	require.Equal(t, "claude-opus-4-6", st.String("default_spawn_model"))
	require.Equal(t, "high", st.String("default_spawn_effort"))
}
