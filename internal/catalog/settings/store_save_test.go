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

// symlinkOrSkip makes link -> target, skipping where symlinks need a
// privilege the test lacks (Windows without developer mode).
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
}

// A config.yaml managed by a dotfiles tool is a symlink: Save writes the
// file it points to and leaves the link in place.
func TestStore_SaveThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	repo := filepath.Join(dir, "dotfiles", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(repo), 0o700))
	require.NoError(t, os.WriteFile(repo, []byte("spinner: moon\n"), 0o600))
	link := filepath.Join(dir, "config", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o700))
	symlinkOrSkip(t, repo, link)

	s := NewStore()
	require.NoError(t, s.Load(link))
	require.NoError(t, s.Set("row_density", "compact"))
	require.NoError(t, s.Save(link))

	fi, err := os.Lstat(link)
	require.NoError(t, err)
	require.NotZero(t, fi.Mode()&os.ModeSymlink, "the link is still a link")
	c := NewStore()
	require.NoError(t, c.Load(repo))
	require.Equal(t, "moon", c.Get("spinner"))
	require.Equal(t, "compact", c.Get("row_density"), "the edit reached the linked file")
	for _, d := range []string{filepath.Dir(repo), filepath.Dir(link)} {
		entries, err := os.ReadDir(d)
		require.NoError(t, err)
		for _, e := range entries {
			require.NotContains(t, e.Name(), ".tmp")
		}
	}
}

// A dangling link is an error; Save never replaces it with a plain file.
func TestStore_SaveRefusesDanglingSymlink(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "config.yaml")
	symlinkOrSkip(t, filepath.Join(dir, "gone", "config.yaml"), link)
	s := NewStore()
	require.NoError(t, s.Set("spinner", "moon"))
	require.Error(t, s.Save(link))
	fi, err := os.Lstat(link)
	require.NoError(t, err)
	require.NotZero(t, fi.Mode()&os.ModeSymlink)
}

// Saves from several Stores at once each succeed: no shared temp file.
func TestStore_SaveConcurrently(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, NewStore().Save(path))
	errs := make(chan error, 8)
	for range 8 {
		go func() {
			s := NewStore()
			if err := s.Set("spinner", "moon"); err != nil {
				errs <- err
				return
			}
			errs <- s.Save(path)
		}()
	}
	for range 8 {
		require.NoError(t, <-errs)
	}
}
