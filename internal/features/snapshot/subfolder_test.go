package snapshot

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// A session in a repo subfolder gets the repo-root .gitignore applied: the
// ignored build output and secrets are not captured, and undo never deletes
// them.
func TestSubfolderSessionHonorsParentGitignore(t *testing.T) {
	repo := newRepo(t)
	write(t, repo, ".gitignore", "node_modules/\n.env\n/web/dist/\n*.log\n")
	write(t, repo, "web/src/a.ts", "a\n")
	write(t, repo, "web/node_modules/dep/index.js", "dep\n")
	write(t, repo, "web/.env", "SECRET=1\n")
	write(t, repo, "web/dist/out.js", "out\n")
	write(t, repo, "web/debug.log", "log\n")
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "-m", "web")
	cwd := repo + "/web"
	s := newStore(t)
	ctx := context.Background()

	tr := capture(t, s, cwd, "s1", PhaseStart)
	ch, err := s.Changed(ctx, cwd, emptyTree, tr)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range ch {
		names = append(names, c.Path)
	}
	all := strings.Join(names, ",")
	for _, bad := range []string{"node_modules", ".env", "dist", "debug.log"} {
		if strings.Contains(all, bad) {
			t.Errorf("ignored %q captured: %s", bad, all)
		}
	}
	if !strings.Contains(all, "src/a.ts") {
		t.Errorf("tracked file missing: %s", all)
	}

	capture(t, s, cwd, "s1", PhaseEnd)
	turn(t, s, cwd, "s1", func() {
		write(t, cwd, "src/a.ts", "changed\n")
		write(t, cwd, "node_modules/dep/new.js", "agent installed\n")
		write(t, cwd, ".env", "SECRET=2\n")
		write(t, cwd, "dist/new.js", "built\n")
		write(t, cwd, "other.log", "log\n")
	})
	if _, err := s.Undo(ctx, cwd, "s1", 1); err != nil {
		t.Fatal(err)
	}
	if read(t, cwd, "src/a.ts") != "a\n" {
		t.Error("tracked file not restored")
	}
	for _, p := range []string{"node_modules/dep/new.js", "dist/new.js", "other.log"} {
		if !exists(cwd, p) {
			t.Errorf("undo deleted ignored file %s", p)
		}
	}
	if read(t, cwd, ".env") != "SECRET=2\n" {
		t.Error("undo touched the ignored .env")
	}
}

func TestGitTooOldAndOwnershipErrorsAreDistinct(t *testing.T) {
	if errors.Is(ErrDubiousOwnership, ErrNotGitWorktree) || errors.Is(ErrGitTooOld, ErrNoGit) {
		t.Fatal("errors must be distinguishable")
	}
	if err := CheckGit(context.Background()); err != nil {
		t.Skipf("git unusable here: %v", err)
	}
}
