package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
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
	require.NoError(t, os.WriteFile(path, []byte("spinner: pulse\nmaster_volume: 0.5\n"), 0o644))

	s := NewStore()
	require.NoError(t, s.Load(path))
	require.Equal(t, "pulse", s.Get("spinner"))
	require.Equal(t, 0.5, s.Get("master_volume"))
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
