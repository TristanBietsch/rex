package settings

import (
	"os"
	"path/filepath"
	"runtime"
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

	// "~/" expands to the cleaned HOME, so the home check must clean it too;
	// it used to skip the check for a HOME holding "..".
	t.Run("home with dot-dot", func(t *testing.T) {
		base := t.TempDir()
		realHome := filepath.Join(base, "home")
		require.NoError(t, os.MkdirAll(filepath.Join(realHome, "dev"), 0o700))
		dotted := filepath.Join(base, "x") + sep + ".." + sep + "home" // built by hand: Join would clean it
		t.Setenv("HOME", dotted)
		t.Setenv("USERPROFILE", dotted)
		for _, entry := range []string{"~/", "~" + sep, realHome} {
			_, err := TrustDir(entry)
			require.ErrorContains(t, err, "home folder", entry)
			_, err = CheckAutoTrustDirs(entry)
			require.ErrorContains(t, err, "home folder", entry)
		}
		_, err := TrustDir("~/dev")
		require.NoError(t, err, "a folder inside it is fine")
	})

	t.Run("home that cannot be resolved", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "gone")
		t.Setenv("HOME", missing)
		t.Setenv("USERPROFILE", missing)
		for _, entry := range []string{"~/", "~/dev", dev} {
			_, err := TrustDir(entry)
			require.ErrorContains(t, err, "cannot check", entry)
		}
	})
}

func TestCanonicalHome(t *testing.T) {
	home, _ := trustHome(t)
	want, err := filepath.EvalSymlinks(home)
	require.NoError(t, err)
	got, err := CanonicalHome()
	require.NoError(t, err)
	require.Equal(t, want, got)

	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	_, err = CanonicalHome()
	require.ErrorIs(t, err, paths.ErrNoHome)
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

func gitDir(t *testing.T, dir string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o700))
	return dir
}

func TestSuggestTrustDir(t *testing.T) {
	home, dev := trustHome(t)
	sep := string(filepath.Separator)
	list := string(filepath.ListSeparator)

	// The repo's own folder, never its parent.
	app := gitDir(t, filepath.Join(dev, "app"))
	require.Equal(t, "~/dev/app", SuggestTrustDir(app), "slashed on every OS")
	// From inside the repo: its root.
	sub := filepath.Join(app, "pkg", "x")
	require.NoError(t, os.MkdirAll(sub, 0o700))
	require.Equal(t, "~/dev/app", SuggestTrustDir(sub))
	// Not a repo: the folder itself.
	plain := filepath.Join(dev, "plain")
	require.NoError(t, os.MkdirAll(plain, 0o700))
	require.Equal(t, "~/dev/plain", SuggestTrustDir(plain))
	// A repo directly in home is a folder directly in home.
	require.Equal(t, "~/app2", SuggestTrustDir(gitDir(t, filepath.Join(home, "app2"))))

	// Never home itself, even when home is a repo (dotfiles) ...
	require.Empty(t, SuggestTrustDir(home))
	gitDir(t, home)
	require.Empty(t, SuggestTrustDir(home), "home is a repo")
	// ... and a plain folder under a home that is a repo is still itself.
	require.Equal(t, "~/dev/plain", SuggestTrustDir(plain))

	// Never root, outside home, empty, relative or with "..".
	require.Empty(t, SuggestTrustDir(filepath.VolumeName(home)+sep))
	require.Empty(t, SuggestTrustDir(gitDir(t, filepath.Join(t.TempDir(), "app"))), "outside home")
	require.Empty(t, SuggestTrustDir(""))
	require.Empty(t, SuggestTrustDir("dev"+sep+"app"), "relative")
	require.Empty(t, SuggestTrustDir(dev+sep+"plain"+sep+".."+sep+"app"), "dotdot")

	// A link inside home that leads out of it is not inside home.
	outside := filepath.Join(t.TempDir(), "code")
	gitDir(t, filepath.Join(outside, "app"))
	require.NoError(t, os.Symlink(outside, filepath.Join(home, "code")))
	require.Empty(t, SuggestTrustDir(filepath.Join(home, "code", "app")), "link out of home")

	// Names that would be read back as other folders.
	require.Empty(t, SuggestTrustDir(gitDir(t, filepath.Join(home, "a"+list, "app"))), "separator in a name")
	require.Empty(t, SuggestTrustDir(gitDir(t, filepath.Join(home, "x"+list+"y"))), "separator in a name")
	if runtime.GOOS != "windows" { // Windows does not allow a trailing space in a name
		require.Empty(t, SuggestTrustDir(gitDir(t, filepath.Join(home, "dev "))), "outer space")
	}
}

func TestAppendTrustDir(t *testing.T) {
	home, dev := trustHome(t)
	sep := string(filepath.ListSeparator)
	a := filepath.Join(dev, "a")
	require.NoError(t, os.MkdirAll(a, 0o700))

	got, added, err := AppendTrustDir("", "~/dev/a")
	require.NoError(t, err)
	require.True(t, added)
	require.Equal(t, "~/dev/a", got)

	got, added, err = AppendTrustDir(got, "~/dev/b")
	require.NoError(t, err)
	require.True(t, added)
	require.Equal(t, "~/dev/a"+sep+"~/dev/b", got, "order kept")

	// Already listed, or covered by a listed parent: no-op.
	for _, dir := range []string{"~/dev/a", a, "~/dev/a/sub", "  ~/dev/b  "} {
		same, added, err := AppendTrustDir(got, dir)
		require.NoError(t, err, dir)
		require.False(t, added, dir)
		require.Equal(t, got, same, dir)
	}
	// A parent of a listed entry is not covered by it.
	got2, added, err := AppendTrustDir(got, "~/dev")
	require.NoError(t, err)
	require.True(t, added)
	require.Equal(t, got+sep+"~/dev", got2)

	// A tidied current value.
	got, _, err = AppendTrustDir(" ~/dev/a "+sep+sep, "~/dev/c")
	require.NoError(t, err)
	require.Equal(t, "~/dev/a"+sep+"~/dev/c", got)

	root := filepath.VolumeName(home) + string(filepath.Separator)
	for name, dir := range map[string]string{
		"blank": " ", "relative": "dev/a", "dotdot": "~/dev/../x", "dotdot abs": dev + "/a/../b",
		"root": root, "home": "~/", "home abs": home, "bare tilde": "~",
		"separator": "~/dev/x" + sep + "~/dev/y",
	} {
		same, added, err := AppendTrustDir("~/dev/a", dir)
		require.Error(t, err, name)
		require.False(t, added, name)
		require.Empty(t, same, name)
	}
	// A current value the daemon would ignore is refused, not carried along.
	_, _, err = AppendTrustDir("rel", "~/dev/a")
	require.Error(t, err)
}

func TestRemoveTrustDir(t *testing.T) {
	_, dev := trustHome(t)
	sep := string(filepath.ListSeparator)
	cur := "~/dev/a" + sep + dev + "/b" + sep + "~/dev/c"

	got, removed, err := RemoveTrustDir(cur, "~/dev/b")
	require.NoError(t, err)
	require.True(t, removed, "absolute entry matched by ~/ form")
	require.Equal(t, "~/dev/a"+sep+"~/dev/c", got)

	got, removed, err = RemoveTrustDir(cur, "~/dev/zzz")
	require.NoError(t, err)
	require.False(t, removed)
	require.Equal(t, cur, got)

	got, removed, err = RemoveTrustDir("~/dev/a", "~/dev/a")
	require.NoError(t, err)
	require.True(t, removed)
	require.Empty(t, got, "empty is allowed: always ask")

	got, removed, err = RemoveTrustDir("", "~/dev/a")
	require.NoError(t, err)
	require.False(t, removed)
	require.Empty(t, got)

	_, _, err = RemoveTrustDir(cur, "~/dev/../a")
	require.Error(t, err)
	_, _, err = RemoveTrustDir(cur, "rel")
	require.Error(t, err)
}

// rex doctor points at `rex trust -rm <entry>` for an entry the daemon
// ignores; that entry must be removable by its text, and the other entries
// must not block the removal.
func TestRemoveTrustDir_RefusedEntryByText(t *testing.T) {
	trustHome(t)
	sep := string(filepath.ListSeparator)
	cur := "~/dev/a" + sep + "~" + sep + "~/dev/c"

	got, removed, err := RemoveTrustDir(cur, "~")
	require.NoError(t, err)
	require.True(t, removed)
	require.Equal(t, "~/dev/a"+sep+"~/dev/c", got)

	got, removed, err = RemoveTrustDir(cur, "~/dev/a")
	require.NoError(t, err, "an ignored entry elsewhere does not block the removal")
	require.True(t, removed)
	require.Equal(t, "~"+sep+"~/dev/c", got)

	_, _, err = RemoveTrustDir(cur, "~/dev/../x")
	require.Error(t, err, "a refused folder that is not listed is still an error")
}

// On a case-insensitive filesystem a case variant names the same folder, so
// removing it must drop the entry instead of saying "not in the list" while
// the folder stays trusted.
func TestRemoveTrustDir_CaseVariant(t *testing.T) {
	_, dev := trustHome(t)
	a := filepath.Join(dev, "proj")
	require.NoError(t, os.MkdirAll(a, 0o700))
	upper := filepath.Join(dev, "PROJ")
	if _, err := os.Stat(upper); err != nil {
		t.Skip("case-sensitive filesystem")
	}
	got, removed, err := RemoveTrustDir("~/dev/proj", upper)
	require.NoError(t, err)
	require.True(t, removed)
	require.Empty(t, got)
}
