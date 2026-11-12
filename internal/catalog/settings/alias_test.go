package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStore_AliasColorSchemeIsRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("color_scheme: noir\n"), 0o600))
	s := NewStore()
	require.NoError(t, s.Load(path))
	require.Equal(t, "noir", s.Get("theme"))
	require.Equal(t, SourceFile, s.Source("theme"))
}

func TestStore_ThemeWinsOverAlias(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("color_scheme: noir\ntheme: paper\n"), 0o600))
	s := NewStore()
	require.NoError(t, s.Load(path))
	require.Equal(t, "paper", s.Get("theme"))
}

func TestStore_SaveWritesOnlyTheCanonicalKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("color_scheme: noir\nspinner: moon\n"), 0o600))
	s := NewStore()
	require.NoError(t, s.Load(path))
	require.NoError(t, s.Set("theme", "paper"))
	require.NoError(t, s.Save(path))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "spinner: moon\ntheme: paper\n", string(raw))
}

func TestStore_SaveUntouchedThemeKeepsOldKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("color_scheme: noir\n"), 0o600))
	s := NewStore()
	require.NoError(t, s.Load(path))
	require.NoError(t, s.Set("spinner", "moon"))
	require.NoError(t, s.Save(path))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "color_scheme: noir\nspinner: moon\n", string(raw))
}

func TestStore_ThemeNameIsValidated(t *testing.T) {
	s := NewStore()
	require.NoError(t, s.Set("theme", "my-theme_2"))
	for _, bad := range []string{"", "Upper", "../x", "has space", "a/b", "-lead"} {
		require.Error(t, s.Set("theme", bad), bad)
	}
}
