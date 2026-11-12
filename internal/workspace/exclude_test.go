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

	require.Equal(t, "/.rex/\n/.rex/audit.jsonl\n", readExclude(t, filepath.Join(root, ".git")))
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
	require.Equal(t, "*.log\n/.rex/\n/.rex/audit.jsonl\n", readExclude(t, gd))
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
	require.Equal(t, "/svc/api/.rex/\n/svc/api/.rex/audit.jsonl\n", readExclude(t, filepath.Join(repo, ".git")))
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
	require.NoError(t, os.WriteFile(filepath.Join(wtGit, "gitdir"), []byte(filepath.Join(wt, ".git")+"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+wtGit+"\n"), 0o600))

	added, err := ExcludeInGit(wt)
	require.NoError(t, err)
	require.True(t, added)
	require.Equal(t, "/.rex/\n/.rex/audit.jsonl\n", readExclude(t, common), "the shared exclude file is used")
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
	require.NoError(t, os.WriteFile(filepath.Join(base, "store", "gitdir"), []byte("../sub/.git\n"), 0o600))

	added, err := ExcludeInGit(sub)
	require.NoError(t, err)
	require.True(t, added)
	require.Equal(t, "/.rex/\n/.rex/audit.jsonl\n", readExclude(t, filepath.Join(base, "store")))
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

func gitFile(t *testing.T, dir, target string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: "+target+"\n"), 0o600))
}

func TestExcludeInGit_GitFileWithoutBackLinkRefused(t *testing.T) {
	base := t.TempDir()
	other := filepath.Join(base, "other")
	fakeGitDir(t, filepath.Join(other, ".git"))
	gitFile(t, filepath.Join(base, "evil"), filepath.Join(other, ".git"))

	_, err := ExcludeInGit(filepath.Join(base, "evil"))
	require.ErrorIs(t, err, ErrUnsafe)
	require.NoDirExists(t, filepath.Join(other, ".git", "info"), "another repo is untouched")
}

func TestExcludeInGit_BackLinkNamingAnotherGitFileRefused(t *testing.T) {
	base := t.TempDir()
	store := filepath.Join(base, "store")
	fakeGitDir(t, store)
	require.NoError(t, os.WriteFile(filepath.Join(store, "gitdir"), []byte(filepath.Join(base, "legit", ".git")+"\n"), 0o600))
	gitFile(t, filepath.Join(base, "evil"), store)

	_, err := ExcludeInGit(filepath.Join(base, "evil"))
	require.ErrorIs(t, err, ErrUnsafe)
	require.NoDirExists(t, filepath.Join(store, "info"))
}

func TestExcludeInGit_HostileCommondirRefused(t *testing.T) {
	for name, commondir := range map[string]string{
		"outside absolute": "OUTSIDE",
		"sibling":          "../sibling",
		"self":             ".",
		"no HEAD":          "NOHEAD",
	} {
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			outside := filepath.Join(base, "outside")
			fakeGitDir(t, outside)
			nohead := filepath.Join(base, "nohead")
			require.NoError(t, os.MkdirAll(nohead, 0o750))
			fakeGitDir(t, filepath.Join(base, "sibling"))
			repo := filepath.Join(base, "repo")
			gd := filepath.Join(repo, ".git")
			fakeGitDir(t, gd)
			switch commondir {
			case "OUTSIDE":
				commondir = outside
			case "NOHEAD":
				commondir = nohead
			}
			require.NoError(t, os.WriteFile(filepath.Join(gd, "commondir"), []byte(commondir+"\n"), 0o600))

			_, err := ExcludeInGit(repo)
			require.ErrorIs(t, err, ErrUnsafe)
			for _, d := range []string{outside, nohead, filepath.Join(base, "sibling"), gd} {
				require.NoDirExists(t, filepath.Join(d, "info"), d)
			}
		})
	}
}

func TestExcludeInGit_SymlinkedInfoDirRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks")
	}
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	gd := filepath.Join(root, ".git")
	fakeGitDir(t, gd)
	outside := filepath.Join(base, "outside")
	require.NoError(t, os.MkdirAll(outside, 0o750))
	require.NoError(t, os.Symlink(outside, filepath.Join(gd, "info")))

	_, err := ExcludeInGit(root)
	require.ErrorIs(t, err, ErrUnsafe)
	entries, _ := os.ReadDir(outside)
	require.Empty(t, entries)
}

func TestExcludeInGit_DirsAndFileArePrivateWhenCreated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("modes")
	}
	root := t.TempDir()
	gd := filepath.Join(root, ".git")
	fakeGitDir(t, gd)
	_, err := ExcludeInGit(root)
	require.NoError(t, err)
	fi, err := os.Stat(filepath.Join(gd, "info"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), fi.Mode().Perm())
	fi, err = os.Stat(filepath.Join(gd, "info", "exclude"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}

func TestExcludeInGit_LongLineBeforeExistingEntryStillCounts(t *testing.T) {
	root := t.TempDir()
	gd := filepath.Join(root, ".git")
	fakeGitDir(t, gd)
	require.NoError(t, os.MkdirAll(filepath.Join(gd, "info"), 0o750))
	content := strings.Repeat("x", 200<<10) + "\n/.rex/\n/.rex/audit.jsonl\n"
	require.NoError(t, os.WriteFile(filepath.Join(gd, "info", "exclude"), []byte(content), 0o600))

	added, err := ExcludeInGit(root)
	require.NoError(t, err)
	require.False(t, added)
}

func TestExcludeInGit_OversizedExcludeRefused(t *testing.T) {
	root := t.TempDir()
	gd := filepath.Join(root, ".git")
	fakeGitDir(t, gd)
	require.NoError(t, os.MkdirAll(filepath.Join(gd, "info"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(gd, "info", "exclude"), []byte(strings.Repeat("a\n", 600<<10)), 0o600))
	_, err := ExcludeInGit(root)
	require.ErrorIs(t, err, ErrUnsafe)
}

func TestExcludeInGit_AddsAuditLineWhenOnlyRexLineExists(t *testing.T) {
	root := t.TempDir()
	gd := filepath.Join(root, ".git")
	fakeGitDir(t, gd)
	require.NoError(t, os.MkdirAll(filepath.Join(gd, "info"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(gd, "info", "exclude"), []byte("/.rex/\n"), 0o600))
	added, err := ExcludeInGit(root)
	require.NoError(t, err)
	require.True(t, added)
	require.Equal(t, "/.rex/\n/.rex/audit.jsonl\n", readExclude(t, gd))
}
