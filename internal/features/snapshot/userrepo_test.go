package snapshot

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Every operation, dry runs and the .env scan included, leaves the user's
// .git folder byte for byte as it was.
func TestUserRepoUntouchedByPlansAndScans(t *testing.T) {
	repo := newRepo(t)
	write(t, repo, ".env", "SECRET=1\n")
	gitDir := filepath.Join(repo, ".git")
	s := newStore(t)
	ctx := context.Background()
	turn(t, s, repo, "s1", func() { write(t, repo, "a.txt", "agent\n") })
	if _, err := s.Undo(ctx, repo, "s1", 1); err != nil {
		t.Fatal(err)
	}
	before := hashTree(t, gitDir)

	if _, err := s.PlanRedo(ctx, repo, "s1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Redo(ctx, repo, "s1"); err != nil {
		t.Fatal(err)
	}
	turn(t, s, repo, "s1", func() { write(t, repo, "a.txt", "again\n") })
	if _, err := s.PlanUndo(ctx, repo, "s1", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := UntrackedEnvFiles(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Usage(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Retain(ctx, RetainOpts{Live: func(string) bool { return true }}); err != nil {
		t.Fatal(err)
	}
	if got := hashTree(t, gitDir); got != before {
		t.Error(".git changed during plan, scan, usage or retention")
	}
}

// Repo-local config that would run a program (an fsmonitor, a hooks path, a
// filter driver) is never executed by any operation.
func TestUserRepoLocalConfigNeverRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell programs need a POSIX shell")
	}
	repo := newRepo(t)
	markers := t.TempDir()
	hookDir := filepath.Join(markers, "hooks")
	if err := os.MkdirAll(hookDir, 0o750); err != nil {
		t.Fatal(err)
	}
	prog := func(name string) string {
		p := filepath.Join(markers, name+".sh")
		//nolint:gosec // a test program must be executable
		if err := os.WriteFile(p, []byte("#!/bin/sh\ncat >/dev/null 2>&1\ntouch '"+filepath.Join(markers, name+".ran")+"'\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, h := range []string{"reference-transaction", "post-checkout", "post-index-change", "pre-commit"} {
		//nolint:gosec // a hook must be executable
		if err := os.WriteFile(filepath.Join(hookDir, h), []byte("#!/bin/sh\ntouch '"+filepath.Join(markers, h+".ran")+"'\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cfg := "[core]\n\tfsmonitor = " + prog("fsmonitor") + "\n\thooksPath = " + hookDir + "\n" +
		"[filter \"spy\"]\n\tclean = " + prog("clean") + "\n\tsmudge = " + prog("smudge") + "\n"
	f, err := os.OpenFile(filepath.Join(repo, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(cfg); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	write(t, repo, ".gitattributes", "*.txt filter=spy\n")
	write(t, repo, "b.txt", "filtered?\n")
	write(t, repo, ".env", "S=1\n")

	// Sanity: plain git runs each configured program, one by one, so each
	// check below can fail. Every marker must fire for the operation named.
	clearMarkers := func() {
		old, _ := filepath.Glob(filepath.Join(markers, "*.ran"))
		for _, f := range old {
			_ = os.Remove(f)
		}
	}
	mustFire := func(what string, markerNames ...string) {
		t.Helper()
		for _, m := range markerNames {
			if _, err := os.Stat(filepath.Join(markers, m+".ran")); err != nil {
				t.Fatalf("plain git %s did not run %s; the test would prove nothing", what, m)
			}
		}
		clearMarkers()
	}
	gitIn(t, repo, "status", "--short")
	mustFire("status", "fsmonitor")
	write(t, repo, "b.txt", "changed for the clean filter\n")
	gitIn(t, repo, "add", "b.txt")
	mustFire("add", "clean", "post-index-change")
	gitIn(t, repo, "update-ref", "refs/probe/x", "HEAD")
	mustFire("update-ref", "reference-transaction")
	if err := os.Remove(filepath.Join(repo, "b.txt")); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "checkout", "--", "b.txt")
	mustFire("checkout", "smudge")
	gitIn(t, repo, "reset", "-q")
	clearMarkers()
	write(t, repo, "b.txt", "filtered?\n")

	s := newStore(t)
	ctx := context.Background()
	turn(t, s, repo, "s1", func() { write(t, repo, "a.txt", "agent\n"); write(t, repo, "b.txt", "agent\n") })
	if _, err := s.PlanUndo(ctx, repo, "s1", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Undo(ctx, repo, "s1", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Redo(ctx, repo, "s1"); err != nil {
		t.Fatal(err)
	}
	if _, err := UntrackedEnvFiles(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Usage(); err != nil {
		t.Fatal(err)
	}
	ran, _ := filepath.Glob(filepath.Join(markers, "*.ran"))
	if len(ran) > 0 {
		t.Errorf("repo-local config ran a program: %v", ran)
	}
}

// A concurrent turn: a capture started while an undo holds the folder waits
// for it and then captures the restored state, not a half-restored one.
func TestCaptureDuringUndoWaitsForIt(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	ctx := context.Background()
	turn(t, s, repo, "s1", func() { write(t, repo, "a.txt", "agent\n"); write(t, repo, "new.txt", "n\n") })
	started := make(chan struct{})
	done := make(chan Tree, 1)
	s.beforeDelete = func() {
		go func() {
			close(started)
			tr, err := s.Capture(ctx, repo, "s2", PhaseStart)
			if err != nil {
				t.Error(err)
			}
			done <- tr
		}()
		<-started
	}
	if _, err := s.Undo(ctx, repo, "s1", 1); err != nil {
		t.Fatal(err)
	}
	tr := <-done
	ch, err := s.Changed(ctx, repo, tr, tr)
	if err != nil || len(ch) != 0 {
		t.Fatal(err)
	}
	// The racing capture saw the finished restore: new.txt is gone from it.
	all, err := s.Changed(ctx, repo, emptyTree, tr)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range all {
		if c.Path == "new.txt" {
			t.Error("the capture ran in the middle of the undo")
		}
	}
}
