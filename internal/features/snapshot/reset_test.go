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

// corruptMiddle damages a ledger on a line that is not the last one.
func corruptMiddle(t *testing.T, dir string) {
	t.Helper()
	b, err := os.ReadFile(ledgerOf(dir))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	lines = append([]string{lines[0], "garbage"}, lines[1:]...)
	//nolint:gosec // the test writes its own temp store
	if err := os.WriteFile(ledgerOf(dir), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func refsOf(t *testing.T, s *Store, repo string) string {
	t.Helper()
	return gitIn(t, shadowOf(s.dirFor(repo)), "for-each-ref", "--format=%(refname)")
}

func TestReadOnlyPathsNeverQuarantine(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	ctx := context.Background()
	turn(t, s, repo, "s1", func() { write(t, repo, "a.txt", "x\n") })
	dir := s.dirFor(repo)
	corruptMiddle(t, dir)

	if _, err := s.Entries(repo); !errors.Is(err, ErrLedgerCorrupt) {
		t.Fatalf("Entries err = %v", err)
	}
	if _, err := s.PlanUndo(ctx, repo, "s1", 1); !errors.Is(err, ErrLedgerCorrupt) {
		t.Fatalf("PlanUndo err = %v", err)
	}
	us, err := s.Usage()
	if err != nil || len(us) != 1 || !us[0].Corrupt {
		t.Fatalf("usage = %+v, err = %v", us, err)
	}
	if m, _ := filepath.Glob(ledgerOf(dir) + ".corrupt-*"); len(m) != 0 {
		t.Errorf("a read-only path renamed the ledger: %v", m)
	}
	if _, err := os.Stat(markerOf(dir)); err == nil {
		t.Error("a read-only path wrote the reset marker")
	}
}

func TestWritePathQuarantinesAndUndoSaysSo(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	ctx := context.Background()
	turn(t, s, repo, "s1", func() { write(t, repo, "a.txt", "x\n") })
	dir := s.dirFor(repo)
	corruptMiddle(t, dir)

	// The daemon's write path (a capture) sets the ledger aside and writes the marker.
	capture(t, s, repo, "s2", PhaseStart)
	m, _ := filepath.Glob(ledgerOf(dir) + ".corrupt-*")
	if len(m) != 1 {
		t.Fatalf("quarantine files = %v", m)
	}
	re := readReset(dir)
	if re == nil || re.File != filepath.Base(m[0]) {
		t.Fatalf("marker = %+v", re)
	}
	// Undo for a session that lost its history names the file and the time.
	_, err := s.Undo(ctx, repo, "s1", 1)
	var reset *ResetError
	if !errors.As(err, &reset) {
		t.Fatalf("err = %v, want *ResetError", err)
	}
	if !strings.Contains(err.Error(), filepath.Base(m[0])) || !strings.Contains(err.Error(), "older turns can't be undone") {
		t.Errorf("message = %q", err.Error())
	}
	if _, err := s.Redo(ctx, repo, "s1"); !errors.As(err, &reset) {
		t.Errorf("redo err = %v", err)
	}
	// The doctor-facing usage reports the reset.
	us, err := s.Usage()
	if err != nil || len(us) != 1 || us[0].ResetAt.IsZero() || us[0].ResetFile != filepath.Base(m[0]) || us[0].Corrupt {
		t.Fatalf("usage = %+v, err = %v", us, err)
	}
	// A session with fresh history still undoes normally.
	capture(t, s, repo, "s2", PhaseEnd)
}

func TestRetainNeverDeletesAResetStoreAndExpiresItByAge(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	ctx := context.Background()
	clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	turn(t, s, repo, "old1", func() { write(t, repo, "a.txt", "x\n") })
	dir := s.dirFor(repo)
	corruptMiddle(t, dir)

	// Retention sees the corruption first: it sets the ledger aside, finds no
	// session, and must not RemoveAll the store (the .corrupt file lives there).
	if _, err := s.Retain(ctx, RetainOpts{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("the store of a reset ledger was deleted")
	}
	if m, _ := filepath.Glob(ledgerOf(dir) + ".corrupt-*"); len(m) != 1 {
		t.Fatalf("the corrupt file is gone: %v", m)
	}
	if !strings.Contains(refsOf(t, s, repo), "refs/rex/old1/") {
		t.Fatal("orphan refs were dropped inside the grace period")
	}
	// Past the age (counted from the reset), the whole store goes.
	reset := readReset(dir)
	if reset == nil {
		t.Fatal("no marker")
	}
	clock = reset.At.Add(8 * 24 * time.Hour)
	rep, err := s.Retain(ctx, RetainOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Dropped) != 1 {
		t.Fatalf("dropped = %v", rep.Dropped)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("an expired reset store was kept")
	}
}

func TestRetainDropsOrphanRefsAfterANewLedgerStarted(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	ctx := context.Background()
	clock := time.Now().UTC()
	s.now = func() time.Time { return clock }
	turn(t, s, repo, "old1", func() { write(t, repo, "a.txt", "x\n") })
	dir := s.dirFor(repo)
	corruptMiddle(t, dir)
	// A new capture recreates the ledger; old1's refs are now orphans.
	turn(t, s, repo, "new1", func() { write(t, repo, "a.txt", "y\n") })
	live := func(id string) bool { return id == "new1" }

	if _, err := s.Retain(ctx, RetainOpts{Live: live}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(refsOf(t, s, repo), "refs/rex/old1/") {
		t.Fatal("orphan refs dropped inside the grace period")
	}
	reset := readReset(dir)
	clock = reset.At.Add(8 * 24 * time.Hour)
	rep, err := s.Retain(ctx, RetainOpts{Live: live})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Dropped) != 1 || !strings.HasSuffix(rep.Dropped[0], "/old1") {
		t.Fatalf("dropped = %v", rep.Dropped)
	}
	refs := refsOf(t, s, repo)
	if strings.Contains(refs, "refs/rex/old1/") || !strings.Contains(refs, "refs/rex/new1/") {
		t.Errorf("refs:\n%s", refs)
	}
}

func TestOrphanRefsAreTheFirstSizeCapCandidates(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	ctx := context.Background()
	big := func(id string) string { return strings.Repeat(id, 50000) + "\n" }
	turn(t, s, repo, "orph", func() { write(t, repo, "o.txt", big("orph")) })
	dir := s.dirFor(repo)
	corruptMiddle(t, dir)
	turn(t, s, repo, "keep", func() { write(t, repo, "k.txt", big("keep")) })
	live := func(id string) bool { return id == "keep" }
	if _, err := s.Retain(ctx, RetainOpts{Live: live}); err != nil {
		t.Fatal(err)
	}
	size := dirSize(dir)
	rep, err := s.Retain(ctx, RetainOpts{Live: live, MaxBytes: size - 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Dropped) == 0 || !strings.HasSuffix(rep.Dropped[0], "/orph") || len(rep.DroppedLive) != 0 {
		t.Fatalf("report = %+v", rep)
	}
}
