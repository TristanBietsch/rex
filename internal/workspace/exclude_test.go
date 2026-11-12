package workspace

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeGitDir makes a minimal git directory (HEAD only) at dir.
func fakeGitDir(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o600))
}

func readExclude(t *testing.T, gitDir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(gitDir, "info", "exclude"))
	require.NoError(t, err)
	return string(b)
}

func TestExcludeInGit_AddsOnceAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	fakeGitDir(t, filepath.Join(root, ".git"))

	added, err := ExcludeInGit(root)
	require.NoError(t, err)
	require.True(t, added)
	added, err = ExcludeInGit(root)
	require.NoError(t, err)
	require.False(t, added)

	require.Equal(t, 1, strings.Count(readExclude(t, filepath.Join(root, ".git")), "/.rex/\n"))
}

func TestExcludeInGit_KeepsExistingLinesAndFixesMissingNewline(t *testing.T) {
	root := t.TempDir()
	gd := filepath.Join(root, ".git")
	fakeGitDir(t, gd)
	require.NoError(t, os.MkdirAll(filepath.Join(gd, "info"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(gd, "info", "exclude"), []byte("*.log"), 0o600))

	added, err := ExcludeInGit(root)
	require.NoError(t, err)
	require.True(t, added)
	require.Equal(t, "*.log\n/.rex/\n", readExclude(t, gd))
}

func TestExcludeInGit_NoRepoIsNoop(t *testing.T) {
	root := t.TempDir()
	added, err := ExcludeInGit(root)
	require.NoError(t, err)
	require.False(t, added)
	require.NoDirExists(t, filepath.Join(root, ".git"))
}

func TestExcludeInGit_SubfolderOfRepoUsesRelativePattern(t *testing.T) {
	repo := t.TempDir()
	fakeGitDir(t, filepath.Join(repo, ".git"))
	sub := filepath.Join(repo, "svc", "api")
	require.NoError(t, os.MkdirAll(sub, 0o750))

	added, err := ExcludeInGit(sub)
	require.NoError(t, err)
	require.True(t, added)
	require.Equal(t, "/svc/api/.rex/\n", readExclude(t, filepath.Join(repo, ".git")))
}

func TestExcludeInGit_GitFilePointingAtGitdir(t *testing.T) {
	base := t.TempDir()
	main := filepath.Join(base, "main")
	common := filepath.Join(main, ".git")
	fakeGitDir(t, common)
	wtGit := filepath.Join(common, "worktrees", "wt")
	fakeGitDir(t, wtGit)
	require.NoError(t, os.WriteFile(filepath.Join(wtGit, "commondir"), []byte("../..\n"), 0o600))
	wt := filepath.Join(base, "wt")
	require.NoError(t, os.MkdirAll(wt, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+wtGit+"\n"), 0o600))

	added, err := ExcludeInGit(wt)
	require.NoError(t, err)
	require.True(t, added)
	require.Equal(t, "/.rex/\n", readExclude(t, common), "the shared exclude file is used")
	added, err = ExcludeInGit(wt)
	require.NoError(t, err)
	require.False(t, added)
}

func TestExcludeInGit_GitFileWithRelativeGitdir(t *testing.T) {
	base := t.TempDir()
	fakeGitDir(t, filepath.Join(base, "store"))
	sub := filepath.Join(base, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(sub, ".git"), []byte("gitdir: ../store\n"), 0o600))

	added, err := ExcludeInGit(sub)
	require.NoError(t, err)
	require.True(t, added)
	require.Equal(t, "/.rex/\n", readExclude(t, filepath.Join(base, "store")))
}

func TestExcludeInGit_HostileGitFileRefused(t *testing.T) {
	root := t.TempDir()
	victim := t.TempDir() // not a git dir: no HEAD
	require.NoError(t, os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: "+victim+"\n"), 0o600))
	_, err := ExcludeInGit(root)
	require.Error(t, err)
	require.NoDirExists(t, filepath.Join(victim, "info"))

	require.NoError(t, os.WriteFile(filepath.Join(root, ".git"), []byte("garbage"), 0o600))
	_, err = ExcludeInGit(root)
	require.Error(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(root, ".git"), []byte(strings.Repeat("x", 5000)), 0o600))
	_, err = ExcludeInGit(root)
	require.Error(t, err)
}

func TestExcludeInGit_SymlinkedExcludeRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	root := t.TempDir()
	gd := filepath.Join(root, ".git")
	fakeGitDir(t, gd)
	require.NoError(t, os.MkdirAll(filepath.Join(gd, "info"), 0o750))
	target := filepath.Join(root, "victim")
	require.NoError(t, os.WriteFile(target, []byte("keep\n"), 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(gd, "info", "exclude")))

	_, err := ExcludeInGit(root)
	require.ErrorIs(t, err, ErrUnsafe)
	b, _ := os.ReadFile(target)
	require.Equal(t, "keep\n", string(b))
}
