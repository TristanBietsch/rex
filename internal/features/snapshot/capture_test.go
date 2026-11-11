package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// hashTree hashes every file under dir (path, mode bits that matter, bytes).
func hashTree(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		h.Write([]byte(rel + "\n"))
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p) //nolint:gosec // test walks its own temp dir
		if err != nil {
			return err
		}
		h.Write(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestUserRepoUntouched(t *testing.T) {
	repo := newRepo(t)
	write(t, repo, "wip.txt", "uncommitted\n")
	gitIn(t, repo, "add", "wip.txt")
	write(t, repo, "a.txt", "two\n")
	gitIn(t, repo, "stash", "push", "-q", "-m", "mine")
	write(t, repo, "a.txt", "three\n")
	gitIn(t, repo, "branch", "side")

	gitDir := filepath.Join(repo, ".git")
	headBefore := gitIn(t, repo, "rev-parse", "HEAD")
	refsBefore := gitIn(t, repo, "for-each-ref")
	stashBefore := gitIn(t, repo, "stash", "list")
	indexBefore, err := os.ReadFile(filepath.Join(gitDir, "index"))
	if err != nil {
		t.Fatal(err)
	}
	allBefore := hashTree(t, gitDir)

	s := newStore(t)
	ctx := context.Background()
	turn(t, s, repo, "sess1", func() {
		write(t, repo, "a.txt", "agent\n")
		write(t, repo, "new.txt", "new\n")
	})
	if _, err := s.Undo(ctx, repo, "sess1", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Redo(ctx, repo, "sess1"); err != nil {
		t.Fatal(err)
	}

	indexAfter, err := os.ReadFile(filepath.Join(gitDir, "index"))
	if err != nil {
		t.Fatal(err)
	}
	if string(indexBefore) != string(indexAfter) {
		t.Error("user's index file changed")
	}
	if got := gitIn(t, repo, "rev-parse", "HEAD"); got != headBefore {
		t.Errorf("HEAD %s -> %s", headBefore, got)
	}
	if got := gitIn(t, repo, "for-each-ref"); got != refsBefore {
		t.Errorf("refs changed:\n%s\n--\n%s", refsBefore, got)
	}
	if got := gitIn(t, repo, "stash", "list"); got != stashBefore {
		t.Errorf("stash changed: %q -> %q", stashBefore, got)
	}
	if got := hashTree(t, gitDir); got != allBefore {
		t.Error(".git contents changed")
	}
}

func TestCaptureRestoreRoundTrip(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	ctx := context.Background()
	write(t, repo, "src/main.go", "package main\n")
	turn(t, s, repo, "s1", func() {
		write(t, repo, "a.txt", "changed\n")
		write(t, repo, "src/main.go", "package other\n")
		write(t, repo, "src/extra/deep.go", "x\n")
	})
	res, err := s.Undo(ctx, repo, "s1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, repo, "a.txt") != "one\n" || read(t, repo, "src/main.go") != "package main\n" {
		t.Error("tracked files not restored")
	}
	if exists(repo, "src/extra/deep.go") || exists(repo, "src/extra") {
		t.Error("file added by the turn (and its empty dir) not removed")
	}
	if len(res.Restored) != 2 || len(res.Deleted) != 1 || res.Deleted[0] != "src/extra/deep.go" {
		t.Errorf("result = %+v", res)
	}
	if res.Safety == "" {
		t.Error("no safety tree")
	}
}

func TestIgnoredFileUntouchedByRestore(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	write(t, repo, "ignored.log", "keep me\n")
	write(t, repo, "build/out.bin", "keep me too\n")
	turn(t, s, repo, "s1", func() {
		write(t, repo, "a.txt", "changed\n")
		write(t, repo, "ignored.log", "agent wrote this\n")
		write(t, repo, "build/other.bin", "new ignored\n")
	})
	if _, err := s.Undo(context.Background(), repo, "s1", 1); err != nil {
		t.Fatal(err)
	}
	if read(t, repo, "ignored.log") != "agent wrote this\n" || !exists(repo, "build/other.bin") || !exists(repo, "build/out.bin") {
		t.Error("an ignored file was touched by restore")
	}
	if read(t, repo, "a.txt") != "one\n" {
		t.Error("tracked file not restored")
	}
}

func TestBigUntrackedSkipped(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	big := strings.Repeat("x", MaxUntrackedBytes+1)
	write(t, repo, "big.bin", big)
	write(t, repo, "small.txt", "s\n")
	turn(t, s, repo, "s1", func() {
		write(t, repo, "a.txt", "changed\n")
		write(t, repo, "big2 [1].bin", big)
	})
	ctx := context.Background()
	tr := capture(t, s, repo, "s1", PhaseSafety)
	ch, err := s.Changed(ctx, repo, "4b825dc642cb6eb9a060e54bf8d69288fbee4904", tr) // the empty tree
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range ch {
		names = append(names, c.Path)
	}
	joined := strings.Join(names, ",")
	if strings.Contains(joined, "big") {
		t.Errorf("big untracked file captured: %s", joined)
	}
	if !strings.Contains(joined, "small.txt") {
		t.Errorf("small file missing: %s", joined)
	}
	// Restore must leave the skipped files alone.
	if _, err := s.Undo(ctx, repo, "s1", 1); err != nil {
		t.Fatal(err)
	}
	if !exists(repo, "big.bin") || !exists(repo, "big2 [1].bin") {
		t.Error("skipped big files were deleted by restore")
	}
}

func TestGitDirNeverCaptured(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	ctx := context.Background()
	tr := capture(t, s, repo, "s1", PhaseStart)
	ch, err := s.Changed(ctx, repo, "4b825dc642cb6eb9a060e54bf8d69288fbee4904", tr)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range ch {
		if strings.HasPrefix(c.Path, ".git/") {
			t.Fatalf("captured %s", c.Path)
		}
	}
}

func TestNestedRepoNotRecursed(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	nested := filepath.Join(repo, "vendor", "lib")
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatal(err)
	}
	gitIn(t, nested, "init", "-q")
	write(t, nested, "inner.txt", "inner\n")
	tr := capture(t, s, repo, "s1", PhaseStart)
	ch, err := s.Changed(context.Background(), repo, "4b825dc642cb6eb9a060e54bf8d69288fbee4904", tr)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range ch {
		if strings.Contains(c.Path, "inner.txt") || strings.Contains(c.Path, "vendor/lib") {
			t.Errorf("nested repo content captured: %s", c.Path)
		}
	}
}

func TestInfoExcludeHonored(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	if err := os.WriteFile(filepath.Join(repo, ".git", "info", "exclude"), []byte("secret.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	write(t, repo, "secret.txt", "s\n")
	tr := capture(t, s, repo, "s1", PhaseStart)
	ch, err := s.Changed(context.Background(), repo, "4b825dc642cb6eb9a060e54bf8d69288fbee4904", tr)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range ch {
		if c.Path == "secret.txt" {
			t.Error("a file in .git/info/exclude was captured")
		}
	}
}

func TestEOLAttributesDoNotConvert(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	write(t, repo, ".gitattributes", "*.txt text eol=crlf\n")
	write(t, repo, "lf.txt", "a\nb\n")
	turn(t, s, repo, "s1", func() { write(t, repo, "lf.txt", "changed\n") })
	if _, err := s.Undo(context.Background(), repo, "s1", 1); err != nil {
		t.Fatal(err)
	}
	if got := read(t, repo, "lf.txt"); got != "a\nb\n" {
		t.Errorf("content converted: %q", got)
	}
}

func TestGlobalHookNotRun(t *testing.T) {
	needGit(t)
	home := fakeHome(t)
	hooks := filepath.Join(home, "hooks")
	marker := filepath.Join(home, "hook-ran")
	if err := os.MkdirAll(hooks, 0o750); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("shell hooks need a POSIX shell")
	}
	// reference-transaction runs on update-ref: the call a capture makes.
	script := "#!/bin/sh\necho ran >> " + marker + "\n"
	for _, h := range []string{"reference-transaction", "pre-commit", "post-checkout", "post-index-change"} {
		//nolint:gosec // a hook must be executable
		if err := os.WriteFile(filepath.Join(hooks, h), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cfg := filepath.Join(home, "gitconfig")
	if err := os.WriteFile(cfg, []byte("[core]\n\thooksPath = "+hooks+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	// Sanity: the hook does fire for a plain update-ref in the user's repo.
	write(t, repo, "a.txt", "1\n")
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "update-ref", "refs/probe/x", gitIn(t, repo, "write-tree"))
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("test hook does not fire; the test proves nothing")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}

	s := newStore(t)
	turn(t, s, repo, "s1", func() { write(t, repo, "a.txt", "2\n") })
	if _, err := s.Undo(context.Background(), repo, "s1", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the user's global git hook ran")
	}
}

func TestNotGitWorktree(t *testing.T) {
	needGit(t)
	fakeHome(t)
	dir := t.TempDir()
	s := newStore(t)
	_, err := s.Capture(context.Background(), dir, "s1", PhaseStart)
	if !errors.Is(err, ErrNotGitWorktree) {
		t.Fatalf("err = %v, want ErrNotGitWorktree", err)
	}
	if _, err := s.Undo(context.Background(), dir, "s1", 1); !errors.Is(err, ErrNotGitWorktree) {
		t.Errorf("Undo err = %v, want ErrNotGitWorktree", err)
	}
	if _, err := s.Redo(context.Background(), dir, "s1"); !errors.Is(err, ErrNotGitWorktree) {
		t.Errorf("Redo err = %v, want ErrNotGitWorktree", err)
	}
	if _, err := os.Stat(s.Root()); err == nil {
		t.Error("a store was created for a non-git folder")
	}
}

func TestStorePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	repo := newRepo(t)
	s := newStore(t)
	capture(t, s, repo, "s1", PhaseStart)
	for _, p := range []string{s.Root(), s.dirFor(repo), shadowOf(s.dirFor(repo))} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o700 {
			t.Errorf("%s mode %v, want 0700", p, fi.Mode().Perm())
		}
	}
}

func TestCaptureValidatesInput(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.Capture(ctx, "relative/dir", "s1", PhaseStart); err == nil {
		t.Error("relative cwd accepted")
	}
	if _, err := s.Capture(ctx, t.TempDir(), "../evil", PhaseStart); err == nil {
		t.Error("bad session id accepted")
	}
	if _, err := s.Capture(ctx, t.TempDir(), "s1", Phase("bogus")); err == nil {
		t.Error("bad phase accepted")
	}
}

func TestGitEnv(t *testing.T) {
	t.Setenv("GIT_DIR", "/evil")
	t.Setenv("GIT_INDEX_FILE", "/evil/idx")
	env := gitEnv("/s/git", "/w", "/s/git/index-x")
	got := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "GIT_") {
			if _, dup := got[k]; dup {
				t.Errorf("%s set twice", k)
			}
			got[k] = v
		}
	}
	want := map[string]string{
		"GIT_DIR":             "/s/git",
		"GIT_WORK_TREE":       "/w",
		"GIT_INDEX_FILE":      "/s/git/index-x",
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_TERMINAL_PROMPT": "0",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	cfg := strings.Join(safeConfig(), " ")
	for _, w := range []string{"core.hooksPath=" + os.DevNull, "core.autocrlf=false"} {
		if !strings.Contains(cfg, w) {
			t.Errorf("safeConfig lacks %s", w)
		}
	}
}

func TestNullDeviceIsPlatformSpecific(t *testing.T) {
	want := "/dev/null"
	if runtime.GOOS == "windows" {
		want = "NUL"
	}
	if os.DevNull != want {
		t.Fatalf("DevNull = %q", os.DevNull)
	}
}

func TestFolderKey(t *testing.T) {
	a := folderKey(filepath.Join(t.TempDir(), "x"))
	if len(a) != 16 {
		t.Errorf("key %q is not 16 chars", a)
	}
	p := filepath.Join(os.TempDir(), "a", "b")
	if folderKey(p) != folderKey(p+string(filepath.Separator)) {
		t.Error("trailing separator changes the key")
	}
	if runtime.GOOS == "windows" {
		if folderKey(`C:\Work\Repo`) != folderKey(`c:\work\repo`) {
			t.Error("windows keys are case sensitive")
		}
	}
}

func TestExcludeLine(t *testing.T) {
	cases := map[string]string{
		"a.txt":       "/a.txt",
		"dir/b [1].x": `/dir/b\ \[1\].x`,
		"star*?":      `/star\*\?`,
		"#hash":       "/#hash",
		"back\\slash": `/back\\slash`,
		"nl\nname":    "",
	}
	for in, want := range cases {
		if got := excludeLine(in); got != want {
			t.Errorf("excludeLine(%q) = %q, want %q", in, got, want)
		}
	}
}
