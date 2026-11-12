package snapshot

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeHome points HOME at an empty temp dir and keeps the user's git config
// out of the test.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	return home
}

func needGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newRepo makes a git repo with one commit and returns its path.
func newRepo(t *testing.T) string {
	t.Helper()
	needGit(t)
	fakeHome(t)
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	write(t, dir, "a.txt", "one\n")
	write(t, dir, ".gitignore", "ignored.log\nbuild/\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func newStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "snapshots"))
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(dir, rel string) bool {
	_, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(rel)))
	return err == nil
}

func capture(t *testing.T, s *Store, cwd, session string, ph Phase) Tree {
	t.Helper()
	tr, err := s.Capture(context.Background(), cwd, session, ph)
	if err != nil {
		t.Fatalf("capture %s: %v", ph, err)
	}
	return tr
}

// turn simulates one agent turn: start capture, mutate, end capture.
func turn(t *testing.T, s *Store, cwd, session string, mutate func()) {
	t.Helper()
	capture(t, s, cwd, session, PhaseStart)
	mutate()
	capture(t, s, cwd, session, PhaseEnd)
}
