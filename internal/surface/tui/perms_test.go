package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/testutil/testperm"
)

// S-01: tui-state.json and its directory were created 0644/0755.
func TestSaveTUIState_ownerOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	require.NoError(t, SaveTUIState(Model{SelectedID: "s1", Filter: "all"}))
	statePath := paths.ForHome(home).TUIStateFile
	testperm.RequireMode(t, filepath.Dir(statePath), 0o700)
	testperm.RequireMode(t, statePath, 0o600)

	sel, filter, ok := LoadTUIState()
	require.True(t, ok)
	require.Equal(t, "s1", sel)
	require.Equal(t, "all", filter)
}

func TestTildePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	sep := string(filepath.Separator)
	require.Equal(t, "~"+sep+".zshrc", tildePath(filepath.Join(home, ".zshrc")))
	require.Equal(t, "~", tildePath(home))
	other := filepath.Join(filepath.Dir(home), "other", ".zshrc")
	require.Equal(t, other, tildePath(other))
}

// Without a home directory the TUI state is neither saved nor loaded:
// never a tui-state.json relative to the working directory.
func TestTUIState_noHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	require.ErrorIs(t, SaveTUIState(Model{SelectedID: "s1"}), paths.ErrNoHome)
	_, _, ok := LoadTUIState()
	require.False(t, ok)

	msg := runBootStep(t, stepPathsEnsure(Model{}))
	require.Equal(t, stepFail, msg.Status)
	require.ErrorIs(t, msg.Err, paths.ErrNoHome)
	require.Empty(t, detectShellRC(), "no shell rc relative to the working directory")
	require.Equal(t, "/x/.zshrc", tildePath("/x/.zshrc"))
}

// S-01, FU-09: the boot splash created ~/.config/rex and
// ~/.local/state/rex 0755 and left an existing 0755 state dir (logs,
// tui-state) readable by everyone.
func TestStepPathsEnsure_ownerOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	msg := runBootStep(t, stepPathsEnsure(Model{}))
	require.Equal(t, stepOK, msg.Status, msg.Desc)
	testperm.RequireMode(t, filepath.Join(home, ".config", "rex"), 0o700)
	testperm.RequireMode(t, filepath.Join(home, ".local", "state", "rex"), 0o700)

	testperm.Loosen(t, filepath.Join(home, ".local", "state", "rex"), 0o755)
	msg = runBootStep(t, stepPathsEnsure(Model{}))
	require.Equal(t, stepOK, msg.Status, msg.Desc)
	testperm.RequireMode(t, filepath.Join(home, ".local", "state", "rex"), 0o700)
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
		testperm.RequireMode(t, rc, mode)
	}

	rc := filepath.Join(dir, "fish", "config.fish")
	require.NoError(t, installShellBlock(rc))
	testperm.RequireMode(t, filepath.Dir(rc), 0o700)
	testperm.RequireMode(t, rc, 0o600)
}
