package snapshot

import (
	"context"
	"errors"
	"testing"
)

func TestPlanChangesNothing(t *testing.T) {
	repo := newRepo(t)
	s := newStore(t)
	ctx := context.Background()
	turn(t, s, repo, "s1", func() {
		write(t, repo, "a.txt", "changed\n")
		write(t, repo, "new.txt", "n\n")
	})
	before, err := s.Entries(repo)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := s.PlanUndo(ctx, repo, "s1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Restored) != 1 || len(plan.Deleted) != 1 || plan.Safety != "" {
		t.Errorf("plan = %+v", plan)
	}
	if read(t, repo, "a.txt") != "changed\n" || !exists(repo, "new.txt") {
		t.Error("PlanUndo changed the folder")
	}
	after, err := s.Entries(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Error("PlanUndo wrote ledger lines")
	}
	if _, err := s.PlanRedo(ctx, repo, "s1"); !errors.Is(err, ErrNothingToRedo) {
		t.Errorf("PlanRedo err = %v", err)
	}
	if _, err := s.Undo(ctx, repo, "s1", 1); err != nil {
		t.Fatal(err)
	}
	plan, err = s.PlanRedo(ctx, repo, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Restored) != 2 {
		t.Errorf("redo plan = %+v", plan)
	}
	if read(t, repo, "a.txt") != "one\n" {
		t.Error("PlanRedo changed the folder")
	}
}
