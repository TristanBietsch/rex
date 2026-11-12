package snapshot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// An undo or redo confirmed against a dry run refuses, before anything
// changes, when the tree it would restore is not the one the preview showed.
func TestUndoRedoWithExpectRefusesStalePreview(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	ctx := context.Background()
	turn(t, s, repo, "s1", func() { write(t, repo, "a.txt", "t1\n") })
	turn(t, s, repo, "s1", func() { write(t, repo, "a.txt", "t2\n") })

	plan, err := s.PlanUndo(ctx, repo, "s1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Target == "" || plan.Safety != "" {
		t.Fatalf("plan = %+v: want a target and no safety capture", plan)
	}
	old, err := s.PlanUndo(ctx, repo, "s1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Undo(ctx, repo, "s1", 1, WithExpect(old.Target)); !errors.Is(err, ErrPreviewStale) {
		t.Fatalf("undo against another preview: err = %v", err)
	}
	if got := read(t, repo, "a.txt"); got != "t2\n" {
		t.Fatalf("a refused undo changed the folder: %q", got)
	}
	res, err := s.Undo(ctx, repo, "s1", 1, WithExpect(plan.Target))
	if err != nil {
		t.Fatal(err)
	}
	if res.Target != plan.Target || read(t, repo, "a.txt") != "t1\n" {
		t.Fatalf("undo: target %s, a.txt %q", res.Target, read(t, repo, "a.txt"))
	}

	redo, err := s.PlanRedo(ctx, repo, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Redo(ctx, repo, "s1", WithExpect(plan.Target)); !errors.Is(err, ErrPreviewStale) {
		t.Fatalf("redo against the undo's preview: err = %v", err)
	}
	if got := read(t, repo, "a.txt"); got != "t1\n" {
		t.Fatalf("a refused redo changed the folder: %q", got)
	}
	if _, err := s.Redo(ctx, repo, "s1", WithExpect(redo.Target)); err != nil {
		t.Fatal(err)
	}
	if got := read(t, repo, "a.txt"); got != "t2\n" {
		t.Fatalf("after redo: %q", got)
	}
}

// diff-tree --raw output: submodule links (mode 160000) on either side and
// malformed records are left out; the status is its first letter.
func TestParseRawSkipsGitlinksAndJunk(t *testing.T) {
	z := "\x00"
	out := ":100644 100644 aaa bbb M" + z + "a.txt" + z +
		":160000 160000 ccc ddd M" + z + "sub" + z +
		":000000 160000 000 eee A" + z + "newsub" + z +
		":100644 000000 fff 000 D" + z + "dir/gone.go" + z +
		":bad" + z + "junk" + z +
		":000000 100755 000 ggg A" + z + "run.sh" + z
	got := parseRaw([]byte(out))
	want := []FileChange{{Path: "a.txt", Status: "M"}, {Path: "dir/gone.go", Status: "D"}, {Path: "run.sh", Status: "A"}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("change %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if parseRaw(nil) != nil {
		t.Fatal("empty output has changes")
	}
}

// Without a reset marker, a leftover turns.jsonl.corrupt-* file still dates
// the reset (retention keeps the store's history around it).
func TestResetTimeFromCorruptLeftover(t *testing.T) {
	dir := t.TempDir()
	if _, ok := resetTime(dir); ok {
		t.Fatal("a clean store has no reset")
	}
	left := ledgerOf(dir) + ".corrupt-1700000000"
	if err := os.WriteFile(left, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	if err := os.Chtimes(left, when, when); err != nil {
		t.Fatal(err)
	}
	got, ok := resetTime(dir)
	if !ok || !got.Equal(when) {
		t.Fatalf("resetTime = %v, %v; want %v", got, ok, when)
	}
}

// A folder store left with no sessions is kept while its ledger was set
// aside as corrupt less than MaxAge ago (the old ledger is the only way to
// recover by hand), and removed once that is older.
func TestRetainKeepsRecentlyResetEmptyStore(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	ctx := context.Background()
	clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	turn(t, s, repo, "gone", func() { write(t, repo, "a.txt", "g\n") })
	if _, err := s.Retain(ctx, RetainOpts{}); err != nil { // marks the orphan
		t.Fatal(err)
	}
	clock = clock.Add(8 * 24 * time.Hour)
	dir := s.dirFor(repo)
	left := ledgerOf(dir) + ".corrupt-1"
	if err := os.WriteFile(left, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	reset := clock.Add(-time.Hour)
	if err := os.Chtimes(left, reset, reset); err != nil {
		t.Fatal(err)
	}
	rep, err := s.Retain(ctx, RetainOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Dropped) != 1 {
		t.Fatalf("dropped = %v", rep.Dropped)
	}
	if _, err := os.Stat(left); err != nil {
		t.Fatalf("a store reset an hour ago was removed: %v", err)
	}
	clock = clock.Add(8 * 24 * time.Hour)
	if _, err := s.Retain(ctx, RetainOpts{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an empty store reset 8 days ago was kept: %v", err)
	}
}

// A long refusal names the first ten paths and counts the rest.
func TestBlockedErrorCapsTheList(t *testing.T) {
	var paths []string
	for i := range 13 {
		paths = append(paths, fmt.Sprintf("f%02d", i))
	}
	msg := (&BlockedError{Paths: paths}).Error()
	if !strings.Contains(msg, "f09 (and 3 more)") || strings.Contains(msg, "f10") {
		t.Fatalf("message: %s", msg)
	}
	if msg := (&BlockedError{Paths: []string{"a"}}).Error(); strings.Contains(msg, "more") {
		t.Fatalf("one path: %s", msg)
	}
}

// A directory at a write path may go only when every file under it is
// about to be deleted; one it cannot fully inspect counts as blocked.
func TestDirOnlyHolds(t *testing.T) {
	cwd := t.TempDir()
	write(t, cwd, "d/a.txt", "a")
	write(t, cwd, "d/sub/b.txt", "b")
	dir := filepath.Join(cwd, "d")
	if !dirOnlyHolds(dir, cwd, map[string]bool{"d/a.txt": true, "d/sub/b.txt": true}) {
		t.Fatal("every file is deleted: the directory empties")
	}
	if dirOnlyHolds(dir, cwd, map[string]bool{"d/a.txt": true}) {
		t.Fatal("d/sub/b.txt stays: blocked")
	}
	if dirOnlyHolds(filepath.Join(cwd, "missing"), cwd, nil) {
		t.Fatal("an unreadable directory is blocked")
	}
	big := filepath.Join(cwd, "big")
	for i := range maxWalked + 1 {
		if err := os.MkdirAll(filepath.Join(big, fmt.Sprintf("d%d", i)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if dirOnlyHolds(big, cwd, nil) {
		t.Fatal("more entries than the preflight inspects: blocked")
	}
}

// A reset marker with an unreadable time is dated by its file instead.
func TestReadResetBadTimeUsesMarkerMtime(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(markerOf(dir), []byte("yesterday turns.jsonl.corrupt-1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	if err := os.Chtimes(markerOf(dir), when, when); err != nil {
		t.Fatal(err)
	}
	re := readReset(dir)
	if re == nil || !re.At.Equal(when) || re.File != "turns.jsonl.corrupt-1" {
		t.Fatalf("reset = %+v", re)
	}
	if !strings.Contains(re.Error(), "turns.jsonl.corrupt-1") {
		t.Fatalf("message: %s", re.Error())
	}
	if got, ok := resetTime(dir); !ok || !got.Equal(when) {
		t.Fatalf("resetTime = %v, %v", got, ok)
	}
}
