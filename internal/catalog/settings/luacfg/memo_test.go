package luacfg

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func bump(t *testing.T, path string) {
	t.Helper()
	future := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(path, future, future))
}

func TestStamp_ChangesWithEditsAndModules(t *testing.T) {
	f := newFixture(t, "rex.set('theme','noir')")
	s0 := Stamp(f.opts)
	require.NotEmpty(t, s0)
	require.Equal(t, s0, Stamp(f.opts))

	f.write("lua/ui.lua", "x = 1")
	s1 := Stamp(f.opts)
	require.NotEqual(t, s0, s1)

	f.write("lua/sub/deep.lua", "x = 1")
	f.write("lua/readme.txt", "ignored")
	s2 := Stamp(f.opts)
	require.NotEqual(t, s1, s2)

	bump(t, filepath.Join(f.dir, "lua", "sub", "deep.lua"))
	require.NotEqual(t, s2, Stamp(f.opts))

	f.write("config.lua", "rex.set('theme','paper')")
	bump(t, f.opts.File)
	require.NotEqual(t, s2, Stamp(f.opts))
}

func TestStamp_MissingFileIsEmptyEvenWithModules(t *testing.T) {
	f := newFixture(t, "")
	f.write("lua/ui.lua", "x = 1")
	require.NoError(t, os.Remove(f.opts.File))
	require.Equal(t, "", Stamp(f.opts))
}

func TestStamp_BadLinkAndDepthAndCount(t *testing.T) {
	skipNoSymlinks(t)
	f := newFixture(t, "x = 1")
	require.NoError(t, os.Symlink("../gone.lua", filepath.Join(f.dir, "lua", "dangling.lua")))
	require.Contains(t, Stamp(f.opts), "dangling.lua:?;")

	f.write("lua/a/b/c/d/e/f/deep.lua", "x = 1")
	require.NotContains(t, Stamp(f.opts), "deep.lua")

	for i := 0; i < maxStampFiles+20; i++ {
		f.write("lua/many/m"+itoa(i)+".lua", "x = 1")
	}
	s := Stamp(f.opts)
	require.Less(t, len(s), 20000)

	g := newFixture(t, "x = 1")
	g.opts.ModuleDir = filepath.Join(g.dir, "absent")
	require.NotEmpty(t, Stamp(g.opts))
	g.opts.ModuleDir = ""
	require.NotEmpty(t, Stamp(g.opts))
}

func TestStamp_UnstatableFile(t *testing.T) {
	skipNoSymlinks(t)
	dir := t.TempDir()
	loop := filepath.Join(dir, "config.lua")
	require.NoError(t, os.Symlink("config.lua", loop))
	require.Contains(t, Stamp(Opts{File: loop}), "?:")
}

func TestLoad_MemoisesByStamp(t *testing.T) {
	f := newFixture(t, "rex.set('theme','noir')")
	r1 := Load(context.Background(), f.opts)
	require.Equal(t, "noir", r1.Values["theme"])

	// Same stamp: the cached result, even though the content differs now.
	st, err := os.Stat(f.opts.File)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(f.opts.File, []byte("rex.set('theme','ansi')"), 0o600))
	require.NoError(t, os.Chtimes(f.opts.File, st.ModTime(), st.ModTime()))
	require.Equal(t, "noir", Load(context.Background(), f.opts).Values["theme"])

	bump(t, f.opts.File)
	require.Equal(t, "ansi", Load(context.Background(), f.opts).Values["theme"])
}

func TestLoad_OneEvalPerStamp(t *testing.T) {
	f := newFixture(t, "rex.set('theme','noir')")
	Load(context.Background(), f.opts)
	before := Touches()
	for i := 0; i < 5; i++ {
		Load(context.Background(), f.opts)
	}
	// Each call stats once (the stamp); none evaluates.
	require.Equal(t, before+5, Touches())
}

func TestLoad_BrokenEditKeepsLastGood(t *testing.T) {
	f := newFixture(t, "rex.set('theme','noir')")
	require.NoError(t, Load(context.Background(), f.opts).Err)

	f.write("config.lua", "rex.set('theme',")
	bump(t, f.opts.File)
	r := Load(context.Background(), f.opts)
	require.Error(t, r.Err)
	require.True(t, r.LastGood)
	require.Equal(t, "noir", r.Values["theme"])

	f.write("config.lua", "rex.set('theme','paper')")
	future := time.Now().Add(2 * time.Hour)
	require.NoError(t, os.Chtimes(f.opts.File, future, future))
	r = Load(context.Background(), f.opts)
	require.NoError(t, r.Err)
	require.False(t, r.LastGood)
	require.Equal(t, "paper", r.Values["theme"])
}

func TestLoad_BrokenAtFirstHasNoValues(t *testing.T) {
	f := newFixture(t, "rex.set(")
	r := Load(context.Background(), f.opts)
	require.Error(t, r.Err)
	require.False(t, r.LastGood)
	require.Empty(t, r.Values)
}

func TestLoad_RemovedFileClearsOverlay(t *testing.T) {
	f := newFixture(t, "rex.set('theme','noir')")
	require.Equal(t, "noir", Load(context.Background(), f.opts).Values["theme"])
	require.NoError(t, os.Remove(f.opts.File))
	r := Load(context.Background(), f.opts)
	require.False(t, r.Exists)
	require.Empty(t, r.Values)
	// And a later broken file has no stale last-good to fall back on.
	f.write("config.lua", "rex.set(")
	r = Load(context.Background(), f.opts)
	require.Error(t, r.Err)
	require.Empty(t, r.Values)
}

func TestLoad_KeyIncludesDenyAndMemoIsBounded(t *testing.T) {
	f := newFixture(t, "rex.set('skip_permissions', true)")
	require.Equal(t, true, Load(context.Background(), f.opts).Values["skip_permissions"])
	denied := f.opts
	denied.Deny = []string{"skip_permissions"}
	require.Empty(t, Load(context.Background(), denied).Values)
	for i := 0; i < 20; i++ {
		o := f.opts
		o.Deny = []string{"x" + itoa(i)}
		Load(context.Background(), o)
	}
	memoMu.Lock()
	n := len(memo)
	memoMu.Unlock()
	require.LessOrEqual(t, n, 16)
}
