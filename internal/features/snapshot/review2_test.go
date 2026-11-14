package snapshot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// A guard that trips while the plan is still being made is seen: the guard
// runs after the plan, right before the first file changes.
func TestGuardFlippingDuringPlanIsSeen(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	turn(t, s, repo, "s1", func() { write(t, repo, "a.txt", "changed\n") })
	var flipped atomic.Bool
	s.afterPlan = func() { flipped.Store(true) } // "the session started working" during the plan
	before := hashTree(t, repo)
	boom := errors.New("session started working")
	_, err := s.Undo(context.Background(), repo, "s1", 1, WithGuard(func() error {
		if flipped.Load() {
			return boom
		}
		return nil
	}))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if hashTree(t, repo) != before {
		t.Error("the folder changed although the guard tripped")
	}
}

// Before the first change a deadline applies and changes nothing; once the
// guard has passed, the restore ignores the caller's cancellation.
func TestDeadlineOnlyCoversThePreChangePhases(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	turn(t, s, repo, "s1", func() { write(t, repo, "a.txt", "changed\n") })

	expired, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	time.Sleep(5 * time.Millisecond)
	before := hashTree(t, repo)
	if _, err := s.Undo(expired, repo, "s1", 1); err == nil {
		t.Fatal("an expired context did not stop the undo")
	}
	if hashTree(t, repo) != before {
		t.Error("a pre-change timeout changed the folder")
	}
	if _, err := s.Redo(context.Background(), repo, "s1"); !errors.Is(err, ErrNothingToRedo) {
		t.Errorf("a pre-change timeout left a redo behind: %v", err)
	}

	ctx, cancelNow := context.WithCancel(context.Background())
	res, err := s.Undo(ctx, repo, "s1", 1, WithGuard(func() error {
		cancelNow() // the client goes away right after the guard passed
		return nil
	}))
	if err != nil {
		t.Fatalf("the restore inherited the cancellation: %v", err)
	}
	if read(t, repo, "a.txt") != "one\n" || len(res.Restored) != 1 {
		t.Errorf("not restored: %+v", res)
	}
}

func TestNewlineInAnIgnoredNameIsStillIgnored(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a newline is not a legal file name character")
	}
	repo := newRepo(t)
	write(t, repo, ".gitignore", "*.log\n")
	s := newStore(t)
	ctx := context.Background()
	name := "a\nb.log"
	write(t, repo, name, "ignored\n")
	tr := capture(t, s, repo, "s1", PhaseStart)
	ch, err := s.Changed(ctx, repo, emptyTree, tr)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range ch {
		if c.Path == name {
			t.Fatal("an ignored file with a newline in its name was captured")
		}
	}
	capture(t, s, repo, "s1", PhaseEnd)
	turn(t, s, repo, "s1", func() { write(t, repo, "a.txt", "x\n"); write(t, repo, name, "agent\n") })
	if _, err := s.Undo(ctx, repo, "s1", 1); err != nil {
		t.Fatal(err)
	}
	if read(t, repo, name) != "agent\n" {
		t.Error("undo touched the ignored file")
	}
}

// Names with glob characters, spaces and a leading '#': the ignored ones are
// neither captured nor touched, the ordinary one comes back, byte for byte.
func TestSpecialNamesRoundTripWithRealGit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("* is not a legal file name character")
	}
	repo := newRepo(t)
	write(t, repo, ".gitignore", "*.env\n\\#hash\n\\ lead\n")
	s := newStore(t)
	ctx := context.Background()
	ignored := []string{"a[1] *.env", "#hash", " lead"}
	const plain = "b[1] *.txt"
	for _, n := range ignored {
		write(t, repo, n, "ignored v1\n")
	}
	write(t, repo, plain, "plain v1\n")
	first := capture(t, s, repo, "s1", PhaseStart)
	ch, err := s.Changed(ctx, repo, emptyTree, first)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range ch {
		names = append(names, c.Path)
	}
	for _, n := range ignored {
		if slices.Contains(names, n) {
			t.Errorf("ignored %q was captured", n)
		}
	}
	if !slices.Contains(names, plain) {
		t.Errorf("%q was not captured: %v", plain, names)
	}
	capture(t, s, repo, "s1", PhaseEnd)
	turn(t, s, repo, "s1", func() {
		write(t, repo, plain, "plain v2\n")
		for _, n := range ignored {
			write(t, repo, n, "ignored v2\n")
		}
	})
	if _, err := s.Undo(ctx, repo, "s1", 1); err != nil {
		t.Fatal(err)
	}
	if read(t, repo, plain) != "plain v1\n" {
		t.Error("the plain file was not restored")
	}
	for _, n := range ignored {
		if read(t, repo, n) != "ignored v2\n" {
			t.Errorf("undo touched ignored %q", n)
		}
	}
}

// When the user repo cannot be listed (a corrupt index), the capture fails
// and writes nothing: no tree, no ref, no ledger line, not even the store.
func TestCaptureFailsCleanlyWhenTheUserRepoCannotBeListed(t *testing.T) {
	repo := newRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".git", "index"), []byte("not an index at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newStore(t)
	if _, err := s.Capture(context.Background(), repo, "s1", PhaseStart); err == nil {
		t.Fatal("capture succeeded with a corrupt user index")
	}
	if _, err := os.Stat(s.Root()); err == nil {
		t.Error("a failed capture created the store")
	}
}

// A capture failing after the store exists leaves its refs and ledger as they
// were.
func TestFailedCaptureLeavesTheStoreAsItWas(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	capture(t, s, repo, "s1", PhaseStart)
	refs := refsOf(t, s, repo)
	entries, err := s.Entries(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".git", "index"), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Capture(context.Background(), repo, "s1", PhaseEnd); err == nil {
		t.Fatal("capture succeeded")
	}
	if got := refsOf(t, s, repo); got != refs {
		t.Errorf("refs changed:\n%s\n--\n%s", refs, got)
	}
	after, _ := s.Entries(repo)
	if len(after) != len(entries) {
		t.Error("a failed capture wrote a ledger line")
	}
}

// On a case-insensitive filesystem a Foo -> foo rename is not a blocker.
func TestCaseOnlyRenameIsNotBlocked(t *testing.T) {
	repo := newRepo(t)
	probe := filepath.Join(repo, "CaseProbe")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(repo, "caseprobe")); err != nil {
		t.Skip("case-sensitive filesystem")
	}
	if err := os.Remove(probe); err != nil {
		t.Fatal(err)
	}
	s := newStore(t)
	ctx := context.Background()
	write(t, repo, "foo.txt", "content\n") // on disk the file is spelled foo.txt
	// The current state, as a tree that spells it foo.txt.
	from := capture(t, s, repo, "s2", PhaseStart)
	shadow := shadowOf(s.dirFor(repo))
	env := gitEnv(shadow, repo, filepath.Join(shadow, "index-pf"))
	// A restore to a state that spells it Foo.txt: write Foo.txt, delete foo.txt.
	if err := preflight(ctx, env, repo, from, []string{"Foo.txt"}, []string{"foo.txt"}); err != nil {
		t.Fatalf("a case-only rename was refused: %v", err)
	}
	// Without the matching delete it is a real conflict.
	var be *BlockedError
	if err := preflight(ctx, env, repo, from, []string{"Foo.txt"}, nil); !errors.As(err, &be) {
		t.Fatalf("err = %v, want *BlockedError", err)
	}
}
