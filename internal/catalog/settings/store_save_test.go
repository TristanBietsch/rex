package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Two Stores (the TUI and `rex config set`) loaded from the same file each
// change a different key: neither save may undo the other's.
func TestStore_SaveKeepsConcurrentEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, NewStore().Save(path))

	a, b := NewStore(), NewStore()
	require.NoError(t, a.Load(path))
	require.NoError(t, b.Load(path))
	require.NoError(t, a.Set("spinner", "moon"))
	require.NoError(t, b.Set("row_density", "compact"))
	require.NoError(t, a.Save(path))
	require.NoError(t, b.Save(path))

	c := NewStore()
	require.NoError(t, c.Load(path))
	require.Equal(t, "moon", c.Get("spinner"))
	require.Equal(t, "compact", c.Get("row_density"))
}

func TestStore_SaveSparse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("spinner: moon\nfuture_key: 7\n"), 0o600))

	s := NewStore()
	require.NoError(t, s.Set("row_density", "compact"))
	require.NoError(t, s.Save(path))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "future_key: 7\nrow_density: compact\nspinner: moon\n", string(raw))

	// Reset deletes the key.
	require.NoError(t, s.Reset("row_density"))
	require.NoError(t, s.Save(path))
	raw, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "future_key: 7\nspinner: moon\n", string(raw))

	// Save cleared the dirty set: a clean Store rewrites nothing.
	require.NoError(t, os.WriteFile(path, []byte("spinner: blocks\n"), 0o600))
	require.NoError(t, s.Save(path))
	raw, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "spinner: blocks\n", string(raw))
}

func TestStore_SaveRefusesUnparseableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("[oops"), 0o600))
	s := NewStore()
	require.NoError(t, s.Set("spinner", "moon"))
	require.Error(t, s.Save(path))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "[oops", string(raw))
}

func TestStore_Source(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("spinner: moon\n"), 0o600))
	s := NewStore()
	require.NoError(t, s.Load(path))
	require.Equal(t, SourceFile, s.Source("spinner"))
	require.Equal(t, SourceDefault, s.Source("row_density"))
	require.NoError(t, s.Set("row_density", "compact"))
	require.Equal(t, SourceFile, s.Source("row_density"))
	require.NoError(t, s.Reset("spinner"))
	require.Equal(t, SourceDefault, s.Source("spinner"))
}

func TestValidate(t *testing.T) {
	v, err := Validate("mouse_enabled", "false")
	require.NoError(t, err)
	require.Equal(t, false, v)
	_, err = Validate("spinner", "spiral")
	require.Error(t, err)
	_, err = Validate("nope", 1)
	require.Error(t, err)
	_, err = Validate("auto_trust_dirs", "/")
	require.Error(t, err)
}

// A Save after Set keeps the file loadable: whatever Load accepts, a Store
// that Sets the same values and saves into a seeded file reloads equal.
func FuzzStoreLoadSaveRoundTrip(f *testing.F) {
	for _, s := range []string{
		"spinner: pulse\nmaster_volume: 0.5\n",
		"unknown_key: 1\nspinner: blocks\n",
		"mouse_enabled: \"false\"\n",
		"",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, data, 0o600))

		s := NewStore()
		if err := s.Load(path); err != nil {
			return
		}
		require.NoError(t, s.Set("spinner", "moon"))
		require.NoError(t, s.Save(path))

		s2 := NewStore()
		require.NoError(t, s2.Load(path))
		requireValidValues(t, s2)
		require.Equal(t, "moon", s2.Get("spinner"))
		for _, set := range Registry {
			if set.ID != "spinner" {
				require.Equal(t, s.Get(set.ID), s2.Get(set.ID), set.ID)
			}
		}
	})
}
