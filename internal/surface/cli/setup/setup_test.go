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
