package snapshot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// requireBlocked runs Undo and requires a *BlockedError naming want, with
// the folder byte for byte unchanged and no redo left behind.
func requireBlocked(t *testing.T, s *Store, repo, want string) {
	t.Helper()
	ctx := context.Background()
	before := hashTree(t, repo)
	_, err := s.Undo(ctx, repo, "s1", 1)
	var be *BlockedError
	if !errors.As(err, &be) {
		t.Fatalf("err = %v, want *BlockedError", err)
	}
	if !strings.Contains(strings.Join(be.Paths, ","), want) || !strings.Contains(err.Error(), want) {
		t.Errorf("blocked paths %v do not name %q", be.Paths, want)
	}
	if hashTree(t, repo) != before {
		t.Error("a refused undo changed the folder")
	}
	if _, err := s.PlanUndo(ctx, repo, "s1", 1); !errors.As(err, &be) {
		t.Errorf("PlanUndo err = %v, want *BlockedError", err)
	}
	if _, err := s.Redo(ctx, repo, "s1"); !errors.Is(err, ErrNothingToRedo) {
		t.Errorf("a refused undo left a redo behind: %v", err)
	}
}

func TestUndoBlockedByNestedRepoAtAFilePath(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	write(t, repo, "vendor", "a file\n")
	turn(t, s, repo, "s1", func() {
		if err := os.Remove(filepath.Join(repo, "vendor")); err != nil {
			t.Fatal(err)
		}
		nested := filepath.Join(repo, "vendor")
		if err := os.MkdirAll(nested, 0o750); err != nil {
			t.Fatal(err)
		}
		gitIn(t, nested, "init", "-q")
		write(t, nested, "work.txt", "never captured\n")
	})
	requireBlocked(t, s, repo, "vendor")
}

func TestUndoBlockedByBigUntrackedFileAtAWritePath(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	write(t, repo, "data.bin", "small\n")
	turn(t, s, repo, "s1", func() {
		if err := os.Remove(filepath.Join(repo, "data.bin")); err != nil {
			t.Fatal(err)
		}
	})
	// After the turn the user drops a big file where the undo wants to write.
	write(t, repo, "data.bin", strings.Repeat("x", MaxUntrackedBytes+10))
	requireBlocked(t, s, repo, "data.bin")
}

func TestUndoBlockedByIgnoredDirAtAFilePath(t *testing.T) {
	repo := newRepo(t) // .gitignore has build/
	s := newStore(t)
	write(t, repo, "build", "a file named like an ignored dir\n")
	turn(t, s, repo, "s1", func() {
		if err := os.Remove(filepath.Join(repo, "build")); err != nil {
			t.Fatal(err)
		}
		write(t, repo, "build/out.bin", "ignored output\n")
	})
	requireBlocked(t, s, repo, "build")
}

func TestUndoBlockedBySymlinkWhereAParentIsNeeded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges")
	}
	repo := newRepo(t)
	s := newStore(t)
	outside := t.TempDir()
	write(t, repo, "pkg/mod.go", "package pkg\n")
	turn(t, s, repo, "s1", func() {
		if err := os.RemoveAll(filepath.Join(repo, "pkg")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(repo, "pkg")); err != nil {
			t.Fatal(err)
		}
	})
	// The undo would write pkg/mod.go through the link into the outside dir.
	// The link was captured, so replacing it is fine only if it is deleted
	// first; either way nothing may land outside.
	_, err := s.Undo(context.Background(), repo, "s1", 1)
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if exists(outside, "mod.go") {
		t.Error("undo wrote through a symlink into an outside folder")
	}
	if read(t, repo, "pkg/mod.go") != "package pkg\n" {
		t.Error("file not restored")
	}
}

func TestUndoDeleteNeverFollowsASwappedParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges")
	}
	repo := newRepo(t)
	s := newStore(t)
	outside := t.TempDir()
	write(t, outside, "f", "outside file\n")
	turn(t, s, repo, "s1", func() { write(t, repo, "d/f", "added by the turn\n") })
	// Between the plan and the delete, the parent dir becomes a link.
	s.beforeDelete = func() {
		if err := os.RemoveAll(filepath.Join(repo, "d")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(repo, "d")); err != nil {
			t.Fatal(err)
		}
	}
	_, err := s.Undo(context.Background(), repo, "s1", 1)
	if err == nil {
		t.Fatal("expected the delete through a link to fail")
	}
	if !strings.Contains(err.Error(), "rex redo") {
		t.Errorf("error should name the recovery: %v", err)
	}
	if read(t, outside, "f") != "outside file\n" {
		t.Error("a delete followed a symlink out of the folder")
	}
}

func TestGuardAbortsBeforeAnyChange(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	ctx := context.Background()
	turn(t, s, repo, "s1", func() { write(t, repo, "a.txt", "changed\n") })
	before := hashTree(t, repo)
	boom := errors.New("session is working")
	_, err := s.Undo(ctx, repo, "s1", 1, WithGuard(func() error { return boom }))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if hashTree(t, repo) != before {
		t.Error("the folder changed although the guard refused")
	}
	if _, err := s.Redo(ctx, repo, "s1"); !errors.Is(err, ErrNothingToRedo) {
		t.Errorf("an aborted undo left a redo behind: %v", err)
	}
	// And the undo still works afterwards.
	if _, err := s.Undo(ctx, repo, "s1", 1); err != nil {
		t.Fatal(err)
	}
}

func TestGuardFlippingMidUndoWarns(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	turn(t, s, repo, "s1", func() { write(t, repo, "a.txt", "changed\n") })
	var calls atomic.Int32
	res, err := s.Undo(context.Background(), repo, "s1", 1, WithGuard(func() error {
		if calls.Add(1) == 2 {
			return errors.New("session started working")
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "started working") {
		t.Errorf("warnings = %v", res.Warnings)
	}
}

func TestFolderChangingDuringRestoreWarns(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	turn(t, s, repo, "s1", func() {
		write(t, repo, "a.txt", "changed\n")
		write(t, repo, "added.txt", "x\n")
	})
	s.beforeDelete = func() { write(t, repo, "late.txt", "written while restoring\n") }
	res, err := s.Undo(context.Background(), repo, "s1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "late.txt") {
		t.Errorf("warnings = %v", res.Warnings)
	}
}

func TestCaptureLimitStartsAfterTheLock(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	capture(t, s, repo, "s1", PhaseStart) // create the store: later captures are fast
	unlock := s.lock(s.dirFor(repo))
	time.AfterFunc(1200*time.Millisecond, unlock)
	start := time.Now()
	if _, err := s.CaptureWithin(context.Background(), repo, "s1", PhaseEnd, 1500*time.Millisecond); err != nil {
		t.Fatalf("capture abandoned after waiting for the lock: %v", err)
	}
	if time.Since(start) < time.Second {
		t.Error("the capture did not wait for the lock")
	}
}

func TestLockWaitEndsWithContext(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	capture(t, s, repo, "s1", PhaseStart)
	unlock := s.lock(s.dirFor(repo))
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := s.Capture(ctx, repo, "s1", PhaseEnd); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}
