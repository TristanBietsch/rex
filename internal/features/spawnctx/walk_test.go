package spawnctx

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// gitRepo makes root a git work tree with local settings only (never the
// user's global config), or skips without git.
func gitRepo(t *testing.T, root string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	cmd := exec.Command("git", "-c", "init.defaultBranch=main", "init", "-q", root)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
}

func TestWalk_CapsEntries(t *testing.T) {
	root := workspace(t, nil)
	for i := range 10001 {
		require.NoError(t, os.WriteFile(filepath.Join(root, fmt.Sprintf("f%05d.txt", i)), nil, 0o600))
	}
	var n atomic.Int64
	idx, err := WalkCounted(context.Background(), root, 10000, time.Minute, &n)
	require.NoError(t, err)
	require.Len(t, idx.Paths, 10000)
	require.True(t, idx.Truncated)
	require.Equal(t, int64(10000), n.Load(), "the count follows the walk")

	idx, err = Walk(context.Background(), root, 20000, time.Minute)
	require.NoError(t, err)
	require.Len(t, idx.Paths, 10001)
	require.False(t, idx.Truncated)
}

func TestWalk_Budget(t *testing.T) {
	root := workspace(t, map[string]string{"a.go": "x", "b.go": "x"})
	idx, err := Walk(context.Background(), root, 100, 0)
	require.NoError(t, err)
	require.Empty(t, idx.Paths)
	require.True(t, idx.Truncated)

	// An injected clock: each reading is 100 ms later, so a 150 ms budget
	// lets the first entries in and stops the walk.
	orig := now
	t.Cleanup(func() { now = orig })
	base := time.Unix(1000, 0)
	var ticks int64
	now = func() time.Time { ticks++; return base.Add(time.Duration(ticks) * 100 * time.Millisecond) }
	idx, err = Walk(context.Background(), root, 100, 150*time.Millisecond)
	require.NoError(t, err)
	require.True(t, idx.Truncated)
	require.Len(t, idx.Paths, 1)
}

func TestWalk_Cancelled(t *testing.T) {
	root := workspace(t, map[string]string{"a.go": "x"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Walk(ctx, root, 100, time.Minute)
	require.ErrorIs(t, err, context.Canceled)

	_, err = Walk(context.Background(), filepath.Join(root, "missing"), 100, time.Minute)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestWalk_PlainSkipsGitAndSpecials(t *testing.T) {
	t.Setenv("PATH", "") // no git: the plain walk
	root := workspace(t, map[string]string{
		".git/HEAD": "ref", "sub/.git": "gitdir: x", "sub/a.go": "x", "README.md": "x",
	})
	idx, err := Walk(context.Background(), root, 100, time.Minute)
	require.NoError(t, err)
	require.Equal(t, []string{"README.md", "sub/", "sub/a.go"}, idx.Paths)
}

func TestWalk_GitIgnore(t *testing.T) {
	root := workspace(t, map[string]string{
		".gitignore": "dist/\n*.log\n", "dist/out.js": "x", "app.log": "x",
		"src/main.go": "x", "src/deep/util.go": "x",
	})
	gitRepo(t, root)
	idx, err := Walk(context.Background(), root, 100, time.Minute)
	require.NoError(t, err)
	require.Equal(t, []string{".gitignore", "src/", "src/deep/", "src/deep/util.go", "src/main.go"}, idx.Paths)
	require.False(t, idx.Truncated)

	idx, err = Walk(context.Background(), root, 2, time.Minute)
	require.NoError(t, err)
	require.True(t, idx.Truncated)
	require.Len(t, idx.Paths, 2)

	// A subfolder of the repo lists its own files, relative to itself.
	idx, err = Walk(context.Background(), filepath.Join(root, "src"), 100, time.Minute)
	require.NoError(t, err)
	require.Equal(t, []string{"deep/", "deep/util.go", "main.go"}, idx.Paths)
}

func TestWalk_GitEnvIgnored(t *testing.T) {
	root := workspace(t, map[string]string{"a.go": "x"})
	gitRepo(t, root)
	other := t.TempDir()
	gitRepo(t, other)
	// A GIT_DIR in the environment must not point the walk elsewhere.
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_WORK_TREE", other)
	idx, err := Walk(context.Background(), root, 100, time.Minute)
	require.NoError(t, err)
	require.Equal(t, []string{"a.go"}, idx.Paths)
}

func TestWalk_SkipsSymlinkedDirs(t *testing.T) {
	t.Setenv("PATH", "") // the plain walk; git never follows links anyway
	root := workspace(t, map[string]string{"real/a.go": "x"})
	outside := workspace(t, map[string]string{"x/y.go": "x"})
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	require.NoError(t, os.Symlink(filepath.Join(root, "real", "a.go"), filepath.Join(root, "file-link.go")))
	require.NoError(t, os.Symlink(filepath.Join(root, "gone"), filepath.Join(root, "dangling")))
	idx, err := Walk(context.Background(), root, 100, time.Minute)
	require.NoError(t, err)
	require.Equal(t, []string{"file-link.go", "real/", "real/a.go"}, idx.Paths)
	require.Equal(t, 1, idx.Links, "the linked folder is counted, not entered")
}

func TestWalk_RootIsALink(t *testing.T) {
	t.Setenv("PATH", "") // the plain walk, which would not enter a link
	real := workspace(t, map[string]string{"a.go": "x"})
	link := filepath.Join(t.TempDir(), "ws")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	idx, err := Walk(context.Background(), link, 100, time.Minute)
	require.NoError(t, err)
	require.Equal(t, []string{"a.go"}, idx.Paths)
}

func TestMatch_Order(t *testing.T) {
	idx := Index{Paths: []string{
		"cmd/rex/auth_test_helpers.go",
		"docs/a/u/t/h.md",
		"internal/auth/",
		"internal/auth/auth_test.go",
		"internal/auth/testdata/",
		"internal/oauthx/server.go",
		"README.md",
	}}
	got := Match(idx, "auth", 0)
	require.Equal(t, []string{
		"internal/auth/",               // basename hit, shortest
		"internal/auth/auth_test.go",   // basename hit
		"cmd/rex/auth_test_helpers.go", // basename hit, longer
		"internal/auth/testdata/",      // path hit
		"internal/oauthx/server.go",    // path hit, longer
		"docs/a/u/t/h.md",              // scattered letters
	}, got)
	require.Equal(t, got[:2], Match(idx, "AUTH", 2), "case-insensitive, limited")
	require.Equal(t, []string{"internal/auth/auth_test.go"}, Match(idx, "authtst", 0)[:1])
	require.Empty(t, Match(idx, "zzz", 0))
	require.Equal(t, []string{"a/auth", "b/auth"}, Match(Index{Paths: []string{"b/auth", "a/auth"}}, "auth", 0), "then by name")
	require.Equal(t, idx.Paths[:3], Match(idx, "", 3), "no query: index order")
	require.Equal(t, idx.Paths, Match(idx, "", 0))
}

func TestIndex_ChildrenAndFiles(t *testing.T) {
	idx := Index{Paths: []string{
		"README.md", "internal/", "internal/auth/", "internal/auth/a.go",
		"internal/auth/testdata/", "internal/auth/testdata/x.json", "internal/main.go",
	}}
	require.Equal(t, []string{"internal/", "README.md"}, Children(idx, ""))
	require.Equal(t, []string{"internal/auth/", "internal/main.go"}, Children(idx, "internal/"))
	require.Equal(t, []string{"internal/auth/", "internal/main.go"}, Children(idx, "internal"))
	require.Equal(t, 3, DirFiles(idx, "internal"))
	require.Equal(t, 2, DirFiles(idx, "internal/auth/"))
	require.Equal(t, 0, DirFiles(idx, "nope"))
}
