package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/testutil/testperm"
)

// requireMode checks path's permission bits on Unix.
func requireMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.Equal(t, want, info.Mode().Perm(), path)
	}
}

// S-01: tui-state.json and its directory were created 0644/0755.
func TestSaveTUIState_ownerOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	require.NoError(t, SaveTUIState(Model{SelectedID: "s1", Filter: "all"}))
	requireMode(t, filepath.Dir(statePath()), 0o700)
	requireMode(t, statePath(), 0o600)
}

// S-01, FU-09: the boot splash created ~/.config/rex and
// ~/.local/state/rex 0755 and left an existing 0755 state dir (logs,
// tui-state) readable by everyone.
func TestStepPathsEnsure_ownerOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	msg := runBootStep(t, stepPathsEnsure(Model{}))
	require.Equal(t, stepOK, msg.Status, msg.Desc)
	requireMode(t, filepath.Join(home, ".config", "rex"), 0o700)
	requireMode(t, filepath.Join(home, ".local", "state", "rex"), 0o700)

	testperm.Loosen(t, filepath.Join(home, ".local", "state", "rex"), 0o755)
	msg = runBootStep(t, stepPathsEnsure(Model{}))
	require.Equal(t, stepOK, msg.Status, msg.Desc)
	requireMode(t, filepath.Join(home, ".local", "state", "rex"), 0o700)
}

// installShellBlock wrote its temp file 0644 and renamed it over the
// profile, so a private ~/.zshrc became world-readable. It keeps the
// profile's mode; a profile it creates is owner-only, in a 0700 directory
// (the XDG mode, e.g. for ~/.config/fish).
func TestInstallShellBlock_modes(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	dir := t.TempDir()
	for _, mode := range []os.FileMode{0o600, 0o644} {
		rc := filepath.Join(dir, "rc-"+mode.String())
		testperm.WriteFile(t, rc, []byte("export SEED=1\n"), mode)
		require.NoError(t, installShellBlock(rc))
		requireMode(t, rc, mode)
	}

	rc := filepath.Join(dir, "fish", "config.fish")
	require.NoError(t, installShellBlock(rc))
	requireMode(t, filepath.Dir(rc), 0o700)
	requireMode(t, rc, 0o600)
}
