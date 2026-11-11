package settings

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
)

// trustHome points the home folder at a fresh temp dir (USERPROFILE too, for
// Windows) and returns it with a ~/dev folder created inside.
func trustHome(t *testing.T) (home, dev string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dev = filepath.Join(home, "dev")
	require.NoError(t, os.MkdirAll(dev, 0o700))
	return home, dev
}

func TestSplitTrustDirs(t *testing.T) {
	home, dev := trustHome(t)
	sep := string(filepath.ListSeparator)

	got, err := SplitTrustDirs("~/dev" + sep + " " + dev + " " + sep)
	require.NoError(t, err)
	require.Equal(t, []string{"~/dev", " " + dev + " ", ""}, got, "pieces come back as split; callers trim")

	got, err = SplitTrustDirs("")
	require.NoError(t, err)
	require.Empty(t, got)

	// A folder name holding the separator splits into a broader folder and
	// a relative piece; the whole value is refused.
	_, err = SplitTrustDirs(filepath.Join(home, "work") + sep + "b")
	require.ErrorContains(t, err, `"b" is not an absolute or ~/ path`)

	_, err = SplitTrustDirs("dev")
	require.ErrorContains(t, err, "not an absolute or ~/ path")

	_, err = SplitTrustDirs("~")
	require.ErrorContains(t, err, "home folder")
}

func TestTrustDir(t *testing.T) {
	home, dev := trustHome(t)
	link := filepath.Join(home, "link")
	require.NoError(t, os.Symlink(dev, link))
	homeLink := filepath.Join(t.TempDir(), "home-link")
	require.NoError(t, os.Symlink(home, homeLink))
	canonDev, ok := CanonicalDir(dev)
	require.True(t, ok)
	sep := string(filepath.Separator)
	root := filepath.VolumeName(home) + sep

	ok1 := []struct{ name, entry, want string }{
		{"absolute", dev, canonDev},
		{"tilde", "~/dev", canonDev},
		{"trimmed", "  ~/dev  ", canonDev},
		{"through a symlink", link, canonDev},
		{"not created yet", "~/dev/new", filepath.Join(canonDev, "new")},
	}
	for _, tc := range ok1 {
		t.Run(tc.name, func(t *testing.T) {
			got, err := TrustDir(tc.entry)
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	refused := []struct{ name, entry, msg string }{
		{"blank", "  ", "no folder given"},
		{"relative", "dev", "not an absolute or ~/ path"},
		{"bare tilde", "~", "not an absolute or ~/ path"},
		{"root", root, "filesystem root"},
		{"home by tilde", "~/", "home folder"},
		{"home by absolute path", home, "home folder"},
		{"home through a symlink", homeLink, "home folder"},
		// Built by hand: filepath.Join would clean the ".." away.
		{"dot-dot", dev + sep + ".." + sep + "x", `".."`},
		{"dot-dot after tilde", "~" + sep + "dev" + sep + ".." + sep + "x", `".."`},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			_, err := TrustDir(tc.entry)
			require.ErrorContains(t, err, tc.msg)
		})
	}

	t.Run("no home", func(t *testing.T) {
		t.Setenv("HOME", "")
		t.Setenv("USERPROFILE", "")
		_, err := TrustDir("~/dev")
		require.ErrorIs(t, err, paths.ErrNoHome)
		got, err := TrustDir(dev)
		require.NoError(t, err, "an absolute folder needs no home to check")
		require.Equal(t, canonDev, got)
	})
}

func TestCheckAutoTrustDirs(t *testing.T) {
	home, dev := trustHome(t)
	sep := string(filepath.ListSeparator)

	got, err := CheckAutoTrustDirs(" ~/dev " + sep + sep + dev)
	require.NoError(t, err)
	require.Equal(t, "~/dev"+sep+dev, got, "pieces trimmed, blanks dropped")

	for name, v := range map[string]string{
		"empty":      "",
		"only blank": " " + sep + " ",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := CheckAutoTrustDirs(v)
			require.ErrorContains(t, err, "no folder given")
		})
	}

	for name, v := range map[string]string{
		"separator in a name": filepath.Join(home, "work") + sep + "b",
		"home in a list":      "~/dev" + sep + "~/",
		"root":                filepath.VolumeName(home) + string(filepath.Separator),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := CheckAutoTrustDirs(v)
			require.Error(t, err)
		})
	}
}

func TestCanonicalDir(t *testing.T) {
	_, dev := trustHome(t)
	canonDev, ok := CanonicalDir(dev)
	require.True(t, ok)
	require.True(t, filepath.IsAbs(canonDev))

	got, ok := CanonicalDir(filepath.Join(dev, "a", "b"))
	require.True(t, ok)
	require.Equal(t, filepath.Join(canonDev, "a", "b"), got, "missing folders keep their names")

	_, ok = CanonicalDir("dev")
	require.False(t, ok, "relative")
	sep := string(filepath.Separator)
	_, ok = CanonicalDir(dev + sep + ".." + sep + "x")
	require.False(t, ok, "dot-dot")
}
