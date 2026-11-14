package snapshot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUndoWalksBackAndRedoForward(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	ctx := context.Background()
	for i, c := range []string{"t1\n", "t2\n", "t3\n"} {
		turn(t, s, repo, "s1", func() { write(t, repo, "a.txt", c) })
		_ = i
	}
	if _, err := s.Undo(ctx, repo, "s1", 1); err != nil {
		t.Fatal(err)
	}
	if got := read(t, repo, "a.txt"); got != "t2\n" {
		t.Fatalf("after 1 undo: %q", got)
	}
	if _, err := s.Undo(ctx, repo, "s1", 1); err != nil {
		t.Fatal(err)
	}
	if got := read(t, repo, "a.txt"); got != "t1\n" {
		t.Fatalf("after 2 undos: %q", got)
	}
	if _, err := s.Redo(ctx, repo, "s1"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, repo, "a.txt"); got != "t2\n" {
		t.Fatalf("after redo: %q", got)
	}
	if _, err := s.Redo(ctx, repo, "s1"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, repo, "a.txt"); got != "t3\n" {
		t.Fatalf("after 2 redos: %q", got)
	}
	if _, err := s.Redo(ctx, repo, "s1"); !errors.Is(err, ErrNothingToRedo) {
		t.Fatalf("third redo err = %v", err)
	}
	// Two turns at once.
	if _, err := s.Undo(ctx, repo, "s1", 2); err != nil {
		t.Fatal(err)
	}
	if got := read(t, repo, "a.txt"); got != "t1\n" {
		t.Fatalf("undo 2 turns: %q", got)
	}
	if _, err := s.Undo(ctx, repo, "s1", 1); err != nil {
		t.Fatal(err)
	}
	if got := read(t, repo, "a.txt"); got != "one\n" {
		t.Fatalf("undo turn 1: %q", got)
	}
	if _, err := s.Undo(ctx, repo, "s1", 1); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("undo past the start err = %v", err)
	}
}

func TestUndoTooManyTurns(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	turn(t, s, repo, "s1", func() { write(t, repo, "a.txt", "x\n") })
	_, err := s.Undo(context.Background(), repo, "s1", 5)
	if !errors.Is(err, ErrNothingToUndo) || !strings.Contains(err.Error(), "only 1") {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.Undo(context.Background(), repo, "s1", 0); err == nil {
		t.Error("turns=0 accepted")
	}
	if got := read(t, repo, "a.txt"); got != "x\n" {
		t.Error("a refused undo changed files")
	}
}

func TestRedoDroppedAfterNewTurn(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	ctx := context.Background()
	turn(t, s, repo, "s1", func() { write(t, repo, "a.txt", "t1\n") })
	if _, err := s.Undo(ctx, repo, "s1", 1); err != nil {
		t.Fatal(err)
	}
	turn(t, s, repo, "s1", func() { write(t, repo, "a.txt", "t2\n") })
	if _, err := s.Redo(ctx, repo, "s1"); !errors.Is(err, ErrNothingToRedo) {
		t.Fatalf("redo after a new turn: %v", err)
	}
	// The new turn is turn 1 again and undoes to the original.
	if _, err := s.Undo(ctx, repo, "s1", 1); err != nil {
		t.Fatal(err)
	}
	if got := read(t, repo, "a.txt"); got != "one\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRedoBringsBackUserEdits(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	ctx := context.Background()
	turn(t, s, repo, "s1", func() { write(t, repo, "a.txt", "agent\n") })
	write(t, repo, "mine.txt", "my own work\n") // after the turn, before undo
	if _, err := s.Undo(ctx, repo, "s1", 1); err != nil {
		t.Fatal(err)
	}
	if exists(repo, "mine.txt") {
		t.Fatal("expected the undo to remove files added since the turn started")
	}
	if _, err := s.Redo(ctx, repo, "s1"); err != nil {
		t.Fatal(err)
	}
	if read(t, repo, "mine.txt") != "my own work\n" || read(t, repo, "a.txt") != "agent\n" {
		t.Error("redo did not restore the safety capture")
	}
}

func TestUndoSessionsAreIndependent(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	turn(t, s, repo, "s1", func() { write(t, repo, "a.txt", "s1\n") })
	if _, err := s.Undo(context.Background(), repo, "s2", 1); !errors.Is(err, ErrNothingToUndo) {
		t.Fatalf("other session's undo: %v", err)
	}
}

func TestLedgerFilesChanged(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	turn(t, s, repo, "s1", func() {
		write(t, repo, "a.txt", "x\n")
		write(t, repo, "b.txt", "y\n")
	})
	es, err := readLedger(s.dirFor(repo))
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 2 || es[0].Phase != PhaseStart || es[1].Phase != PhaseEnd {
		t.Fatalf("ledger = %+v", es)
	}
	if es[1].FilesChanged != 2 || es[0].Turn != 1 || es[1].Turn != 1 {
		t.Errorf("end entry = %+v", es[1])
	}
	if es[0].SessionID != "s1" || es[0].Tree == "" || es[0].At.IsZero() {
		t.Errorf("start entry = %+v", es[0])
	}
}

func TestFold(t *testing.T) {
	e := func(ph Phase, turn int, tree string) Entry {
		return Entry{SessionID: "s", Phase: ph, Turn: turn, Tree: Tree(tree)}
	}
	p := fold([]Entry{
		e(PhaseStart, 1, "a"), e(PhaseEnd, 1, "b"),
		e(PhaseStart, 2, "c"), e(PhaseEnd, 2, "d"),
		e(PhaseSafety, 2, "e"), e(phaseUndo, 1, "c"),
		{SessionID: "other", Phase: PhaseStart, Turn: 9, Tree: "z"},
	}, "s")
	if p.cursor != 1 || len(p.undone) != 1 || p.starts[2] != "c" || p.last != "e" {
		t.Errorf("fold = %+v", p)
	}
	p = fold([]Entry{
		e(PhaseStart, 1, "a"), e(PhaseStart, 2, "c"), e(PhaseSafety, 2, "e"), e(phaseUndo, 1, "c"),
		e(phaseRedo, 2, "e"),
	}, "s")
	if p.cursor != 2 || len(p.undone) != 0 {
		t.Errorf("after redo: %+v", p)
	}
	p = fold([]Entry{
		e(PhaseStart, 1, "a"), e(PhaseStart, 2, "c"), e(PhaseSafety, 2, "e"), e(phaseUndo, 1, "c"),
		e(PhaseStart, 2, "f"),
	}, "s")
	if p.cursor != 2 || len(p.undone) != 0 || p.starts[2] != "f" {
		t.Errorf("after new turn: %+v", p)
	}
}

func TestRestoreRejectsBadInput(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	ctx := context.Background()
	if _, err := s.Restore(ctx, repo, "", RestoreOpts{Session: "s1"}); err == nil {
		t.Error("empty trees accepted")
	}
	if _, err := s.Restore(ctx, repo, "abc", RestoreOpts{Session: "../x", From: "def"}); err == nil {
		t.Error("bad session accepted")
	}
	if _, err := s.Restore(ctx, "rel", "a", RestoreOpts{Session: "s1", From: "b"}); err == nil {
		t.Error("relative cwd accepted")
	}
	if _, err := s.Changed(ctx, repo, "a", "b"); err == nil {
		t.Error("Changed on a folder without a store succeeded")
	}
}

func TestRestoreFileDirSwap(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	write(t, repo, "thing", "file\n")
	turn(t, s, repo, "s1", func() {
		if err := os.Remove(filepath.Join(repo, "thing")); err != nil {
			t.Fatal(err)
		}
		write(t, repo, "thing/inner.txt", "dir\n")
	})
	if _, err := s.Undo(context.Background(), repo, "s1", 1); err != nil {
		t.Fatal(err)
	}
	if read(t, repo, "thing") != "file\n" {
		t.Error("file/dir swap not undone")
	}
}

func TestMarkRemovedNoStore(t *testing.T) {
	s := newStore(t)
	if err := s.MarkRemoved(context.Background(), t.TempDir(), "s1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Root()); err == nil {
		t.Error("MarkRemoved created a store")
	}
}

func TestRetainRemovesOnlyLongPrunedSessions(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	ctx := context.Background()
	clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	turn(t, s, repo, "old", func() { write(t, repo, "a.txt", "old\n") })
	turn(t, s, repo, "recent", func() { write(t, repo, "a.txt", "recent\n") })
	turn(t, s, repo, "live", func() { write(t, repo, "a.txt", "live\n") })
	if err := s.MarkRemoved(context.Background(), repo, "old"); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(5 * 24 * time.Hour)
	if err := s.MarkRemoved(context.Background(), repo, "recent"); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(3 * 24 * time.Hour) // old: 8 days, recent: 3 days
	rep, err := s.Retain(ctx, RetainOpts{Live: func(id string) bool { return id == "live" }})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Dropped) != 1 || !strings.HasSuffix(rep.Dropped[0], "/old") {
		t.Fatalf("dropped = %v", rep.Dropped)
	}
	refs := gitIn(t, shadowOf(s.dirFor(repo)), "for-each-ref", "--format=%(refname)")
	if strings.Contains(refs, "refs/rex/old/") {
		t.Errorf("old refs remain:\n%s", refs)
	}
	for _, id := range []string{"recent", "live"} {
		if !strings.Contains(refs, "refs/rex/"+id+"/") {
			t.Errorf("%s refs were removed:\n%s", id, refs)
		}
	}
	if rep.GCRuns != 1 {
		t.Errorf("gc runs = %d", rep.GCRuns)
	}
	// The kept sessions still undo.
	if _, err := s.Undo(ctx, repo, "recent", 1); err != nil {
		t.Fatal(err)
	}
	// gc runs at most once per day: a second pass does nothing.
	rep, err = s.Retain(ctx, RetainOpts{Live: func(id string) bool { return id == "live" }})
	if err != nil {
		t.Fatal(err)
	}
	if rep.GCRuns != 0 || len(rep.Dropped) != 0 {
		t.Errorf("second pass = %+v", rep)
	}
}

func TestRetainMarksOrphansThenDropsThem(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	ctx := context.Background()
	clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	turn(t, s, repo, "gone", func() { write(t, repo, "a.txt", "g\n") })
	if rep, err := s.Retain(ctx, RetainOpts{}); err != nil || len(rep.Dropped) != 0 {
		t.Fatalf("first pass = %+v, %v", rep, err)
	}
	clock = clock.Add(8 * 24 * time.Hour)
	rep, err := s.Retain(ctx, RetainOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Dropped) != 1 {
		t.Fatalf("dropped = %v", rep.Dropped)
	}
	if _, err := os.Stat(s.dirFor(repo)); err == nil {
		t.Error("a folder store with no sessions was kept")
	}
}

func TestRetainSizeCapDropsOldestFirst(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	for _, id := range []string{"first", "second", "third"} {
		blob := strings.Repeat(id, 50000) + "\n"
		turn(t, s, repo, id, func() { write(t, repo, "f-"+id+".txt", blob) })
		clock = clock.Add(time.Hour)
	}
	live := func(string) bool { return true }
	// A first pass packs the store (the daily gc), so the size is final.
	if _, err := s.Retain(context.Background(), RetainOpts{Live: live}); err != nil {
		t.Fatal(err)
	}
	before := dirSize(s.dirFor(repo))
	// A cap just under the packed size must drop the oldest session.
	rep, err := s.Retain(context.Background(), RetainOpts{Live: live, MaxBytes: before - 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Dropped) == 0 || !strings.HasSuffix(rep.Dropped[0], "/first") {
		t.Fatalf("dropped = %v (size %d)", rep.Dropped, before)
	}
	refs := gitIn(t, shadowOf(s.dirFor(repo)), "for-each-ref", "--format=%(refname)")
	if strings.Contains(refs, "refs/rex/first/") || !strings.Contains(refs, "refs/rex/third/") {
		t.Errorf("refs:\n%s", refs)
	}
}

func TestUsageAndEnvFiles(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	if us, err := s.Usage(); err != nil || us != nil {
		t.Fatalf("empty usage = %v, %v", us, err)
	}
	write(t, repo, ".env", "SECRET=1\n")
	write(t, repo, "app/.env.local", "SECRET=2\n")
	write(t, repo, "notenv.txt", "x\n")
	capture(t, s, repo, "s1", PhaseStart)
	us, err := s.Usage()
	if err != nil || len(us) != 1 {
		t.Fatalf("usage = %v, %v", us, err)
	}
	if us[0].CWD != repo || us[0].Bytes <= 0 || us[0].Sessions != 1 || len(us[0].Key) != 16 {
		t.Errorf("usage = %+v", us[0])
	}
	files, err := UntrackedEnvFiles(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(files, ",") != ".env,app/.env.local" {
		t.Errorf("env files = %v", files)
	}
	if _, err := UntrackedEnvFiles(context.Background(), t.TempDir()); !errors.Is(err, ErrNotGitWorktree) {
		t.Errorf("non-git: %v", err)
	}
}

func TestMarkMissedMakesTurnNotUndoable(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	ctx := context.Background()
	turn(t, s, repo, "s1", func() { write(t, repo, "a.txt", "t1\n") })
	if err := s.MarkMissed(context.Background(), repo, "s1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Undo(ctx, repo, "s1", 1); !errors.Is(err, ErrNotUndoable) {
		t.Fatalf("err = %v, want ErrNotUndoable", err)
	}
	if got := read(t, repo, "a.txt"); got != "t1\n" {
		t.Error("a refused undo changed files")
	}
	// The next real turn is numbered after the missed one and undoes alone.
	turn(t, s, repo, "s1", func() { write(t, repo, "a.txt", "t3\n") })
	if _, err := s.Undo(ctx, repo, "s1", 1); err != nil {
		t.Fatal(err)
	}
	if got := read(t, repo, "a.txt"); got != "t1\n" {
		t.Errorf("got %q", got)
	}
	if err := s.MarkMissed(context.Background(), "rel", "s1"); err == nil {
		t.Error("relative cwd accepted")
	}
}
