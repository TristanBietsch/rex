package snapshot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLedgerTornLastLineIsSkippedAndRepaired(t *testing.T) {
	dir := t.TempDir()
	ok1 := `{"session_id":"s","turn":1,"phase":"start","tree":"aaa","at":"2026-11-01T00:00:00Z","files_changed":0}`
	if err := os.WriteFile(ledgerOf(dir), []byte(ok1+"\n"+`{"session_id":"s","tu`), 0o600); err != nil {
		t.Fatal(err)
	}
	es, err := readLedger(dir)
	if err != nil || len(es) != 1 {
		t.Fatalf("entries = %v, err = %v", es, err)
	}
	// An append starts on a fresh line: the torn tail is gone, not glued on.
	if err := appendLedger(dir, Entry{SessionID: "s", Turn: 1, Phase: PhaseEnd, Tree: "bbb", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	es, err = readLedger(dir)
	if err != nil || len(es) != 2 || es[1].Phase != PhaseEnd {
		t.Fatalf("after append: %v, err = %v", es, err)
	}
	matches, _ := filepath.Glob(ledgerOf(dir) + ".corrupt-*")
	if len(matches) != 0 {
		t.Errorf("a torn tail must not quarantine the file: %v", matches)
	}
}

func TestLedgerCompleteGarbageLastLineAlsoRepairs(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(ledgerOf(dir), []byte("garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if es, err := readLedger(dir); err != nil || len(es) != 0 {
		t.Fatalf("entries = %v, err = %v", es, err)
	}
	if err := appendLedger(dir, Entry{SessionID: "s", Phase: PhaseStart, Turn: 1, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if es, err := readLedger(dir); err != nil || len(es) != 1 {
		t.Fatalf("after append: %v, err = %v", es, err)
	}
}

func TestLedgerCorruptionInTheMiddleIsQuarantined(t *testing.T) {
	dir := t.TempDir()
	ok := `{"session_id":"s","turn":1,"phase":"start","tree":"aaa","at":"2026-11-01T00:00:00Z","files_changed":0}`
	if err := os.WriteFile(ledgerOf(dir), []byte(ok+"\nnot json\n"+ok+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	es, err := readLedger(dir)
	if err != nil || len(es) != 0 {
		t.Fatalf("entries = %v, err = %v", es, err)
	}
	matches, _ := filepath.Glob(ledgerOf(dir) + ".corrupt-*")
	if len(matches) != 1 {
		t.Fatalf("quarantine files = %v", matches)
	}
	if _, err := os.Stat(ledgerOf(dir)); err == nil {
		t.Error("the corrupt ledger was left in place")
	}
	if err := appendLedger(dir, Entry{SessionID: "s", Phase: PhaseStart, Turn: 1, At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if es, err := readLedger(dir); err != nil || len(es) != 1 {
		t.Fatalf("fresh ledger: %v, err = %v", es, err)
	}
}

func TestRetainContinuesPastABadFolder(t *testing.T) {
	repoA, repoB := newRepo(t), newRepo(t)
	s := newStore(t)
	clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	turn(t, s, repoA, "a", func() { write(t, repoA, "a.txt", "x\n") })
	turn(t, s, repoB, "b", func() { write(t, repoB, "a.txt", "y\n") })
	// First pass marks both sessions removed.
	if _, err := s.Retain(context.Background(), RetainOpts{}); err != nil {
		t.Fatal(err)
	}
	// Make folder A's ledger unreadable (a directory where the file is).
	ledgerA := ledgerOf(s.dirFor(repoA))
	if err := os.Remove(ledgerA); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(ledgerA, 0o700); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(8 * 24 * time.Hour)
	rep, err := s.Retain(context.Background(), RetainOpts{})
	if err == nil {
		t.Error("the bad folder's error was swallowed")
	}
	found := false
	for _, d := range rep.Dropped {
		found = found || strings.HasSuffix(d, "/b")
	}
	if !found {
		t.Errorf("the good folder was not retained: %+v", rep)
	}
}

func TestCapDropsRemovedSessionsBeforeLiveOnes(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	// "live" is the oldest, "gone" is newer but has left the board.
	for _, id := range []string{"live", "gone"} {
		blob := strings.Repeat(id, 50000) + "\n"
		turn(t, s, repo, id, func() { write(t, repo, "f-"+id+".txt", blob) })
		clock = clock.Add(time.Hour)
	}
	if err := s.MarkRemoved(repo, "gone"); err != nil {
		t.Fatal(err)
	}
	liveOnly := func(id string) bool { return id == "live" }
	if _, err := s.Retain(context.Background(), RetainOpts{Live: liveOnly}); err != nil {
		t.Fatal(err)
	}
	size := dirSize(s.dirFor(repo))
	rep, err := s.Retain(context.Background(), RetainOpts{Live: liveOnly, MaxBytes: size - 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Dropped) == 0 || !strings.HasSuffix(rep.Dropped[0], "/gone") || len(rep.DroppedLive) != 0 {
		t.Fatalf("report = %+v", rep)
	}
}

func TestCapNeverEmptiesAStoreWithALiveSession(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	blob := strings.Repeat("z", 200000) + "\n"
	turn(t, s, repo, "only", func() { write(t, repo, "big.txt", blob) })
	live := func(string) bool { return true }
	if _, err := s.Retain(context.Background(), RetainOpts{Live: live}); err != nil {
		t.Fatal(err)
	}
	rep, err := s.Retain(context.Background(), RetainOpts{Live: live, MaxBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Dropped) != 0 || len(rep.DroppedLive) != 0 {
		t.Fatalf("the only live session was dropped: %+v", rep)
	}
	if _, err := os.Stat(s.dirFor(repo)); err != nil {
		t.Fatal("the store of a live session was deleted")
	}
	if _, err := s.Undo(context.Background(), repo, "only", 1); err != nil {
		t.Fatalf("live session can no longer undo: %v", err)
	}
}

func TestCapLiveSessionsLastResortIsRecorded(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return clock }
	for _, id := range []string{"older", "newer"} {
		blob := strings.Repeat(id, 50000) + "\n"
		turn(t, s, repo, id, func() { write(t, repo, "f-"+id+".txt", blob) })
		clock = clock.Add(time.Hour)
	}
	live := func(string) bool { return true }
	if _, err := s.Retain(context.Background(), RetainOpts{Live: live}); err != nil {
		t.Fatal(err)
	}
	rep, err := s.Retain(context.Background(), RetainOpts{Live: live, MaxBytes: dirSize(s.dirFor(repo)) - 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.DroppedLive) != 1 || !strings.HasSuffix(rep.DroppedLive[0], "/older") {
		t.Fatalf("report = %+v", rep)
	}
	us, err := s.Usage()
	if err != nil || len(us) != 1 || len(us[0].OverCap) != 1 || !strings.Contains(us[0].OverCap[0], "older") {
		t.Fatalf("usage = %+v, err = %v", us, err)
	}
}
