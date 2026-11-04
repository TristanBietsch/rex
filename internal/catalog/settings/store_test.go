package settings

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/testutil/testperm"
)

func TestStore_DefaultsPopulated(t *testing.T) {
	s := NewStore()
	require.Equal(t, "default", s.Get("color_scheme"))
	require.Equal(t, "braille", s.Get("spinner"))
	require.Equal(t, true, s.Get("mouse_enabled"))
	require.Equal(t, 16, s.Get("max_concurrent_sessions"))
}

func TestStore_SetAndGet(t *testing.T) {
	s := NewStore()
	require.NoError(t, s.Set("spinner", "moon"))
	require.Equal(t, "moon", s.Get("spinner"))
}

func TestStore_RejectsUnknownEnumOption(t *testing.T) {
	s := NewStore()
	require.Error(t, s.Set("spinner", "spiral"))
}

func TestStore_RejectsOutOfRangeFloat(t *testing.T) {
	s := NewStore()
	require.Error(t, s.Set("master_volume", 1.5))
}

func TestStore_StringParsedToBool(t *testing.T) {
	s := NewStore()
	require.NoError(t, s.Set("mouse_enabled", "false"))
	require.Equal(t, false, s.Get("mouse_enabled"))
}

func TestStore_LoadOverridesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("spinner: pulse\nmaster_volume: 0.5\n"), 0o600))

	s := NewStore()
	require.NoError(t, s.Load(path))
	require.Equal(t, "pulse", s.Get("spinner"))
	require.Equal(t, 0.5, s.Get("master_volume"))
}

func TestLoadDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	want := paths.ForHome(home).ConfigFile

	s, path, err := LoadDefault()
	require.NoError(t, err, "a missing config file is not an error")
	require.Equal(t, want, path)
	require.Equal(t, "braille", s.Get("spinner"))

	testperm.MkdirAll(t, filepath.Dir(want), 0o700)
	require.NoError(t, os.WriteFile(want, []byte("spinner: moon\n"), 0o600))
	s, _, err = LoadDefault()
	require.NoError(t, err)
	require.Equal(t, "moon", s.Get("spinner"))

	require.NoError(t, os.WriteFile(want, []byte("spinner: [\n"), 0o600))
	s, path, err = LoadDefault()
	require.Error(t, err)
	require.Equal(t, want, path)
	require.Equal(t, "braille", s.Get("spinner"), "a bad file leaves the defaults")

	// No home: defaults, no path, and Save refuses rather than writing
	// into the working directory.
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	s, path, err = LoadDefault()
	require.ErrorIs(t, err, paths.ErrNoHome)
	require.Empty(t, path)
	require.Equal(t, "braille", s.Get("spinner"))
	require.ErrorIs(t, s.Save(path), errNoPath)
}

func TestStore_SaveAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	s := NewStore()
	require.NoError(t, s.Set("spinner", "blocks"))
	require.NoError(t, s.Save(path))

	s2 := NewStore()
	require.NoError(t, s2.Load(path))
	require.Equal(t, "blocks", s2.Get("spinner"))
}

func TestStore_ReadOnlyRejected(t *testing.T) {
	s := NewStore()
	require.Error(t, s.Set("lua_config_path", "/tmp/foo"))
}

func TestStore_Reset(t *testing.T) {
	s := NewStore()
	require.NoError(t, s.Set("spinner", "moon"))
	require.NoError(t, s.Reset("spinner"))
	require.Equal(t, "braille", s.Get("spinner"))
}

// requireMode checks path's permission bits (Unix only: Windows has just a
// read-only attribute).
func requireMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.Equal(t, want, info.Mode().Perm(), path)
	}
}

// S-01: Save created ~/.config/rex 0755 and config.yaml 0644, and its
// rename replaced a private config.yaml with a world-readable one. A new
// file is owner-only; an existing one keeps the mode its owner chose.
func TestStore_SaveModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rex")
	path := filepath.Join(dir, "config.yaml")
	s := NewStore()

	require.NoError(t, s.Save(path))
	requireMode(t, dir, 0o700)
	requireMode(t, path, 0o600)

	for _, mode := range []os.FileMode{0o600, 0o640, 0o644} {
		testperm.Loosen(t, path, mode)
		require.NoError(t, s.Set("spinner", "blocks"))
		require.NoError(t, s.Save(path))
		requireMode(t, path, mode)
	}
}
