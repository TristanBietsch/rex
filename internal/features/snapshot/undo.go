package snapshot

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// Errors from Undo and Redo, matched with errors.Is.
var (
	// ErrNothingToUndo means the session has no recorded turn left to undo.
	ErrNothingToUndo = errors.New("nothing to undo: no earlier turn is recorded for this session")
	// ErrNothingToRedo means no undo is waiting to be redone.
	ErrNothingToRedo = errors.New("nothing to redo")
	// ErrNotUndoable means the wanted turn has no usable start capture (the
	// capture failed or timed out).
	ErrNotUndoable = errors.New("that turn was not captured, so it cannot be undone")
)

// UndoResult is what Undo and Redo did.
type UndoResult struct {
	Restored []string
	Deleted  []string
	// Safety is the capture taken just before the restore. After Undo it is
	// what Redo brings back.
	Safety Tree
}

// Undo restores cwd to how it was before session's last turns turns, after a
// safety capture of the current state. Repeated calls walk further back.
// The caller checks that the session is not working and that no other live
// session shares the folder.
func (s *Store) Undo(ctx context.Context, cwd, session string, turns int) (UndoResult, error) {
	return s.undo(ctx, cwd, session, turns, false)
}

// PlanUndo is Undo without doing it: it lists the files Undo would write and
// delete and changes nothing in the folder or its history (Safety is empty).
func (s *Store) PlanUndo(ctx context.Context, cwd, session string, turns int) (UndoResult, error) {
	return s.undo(ctx, cwd, session, turns, true)
}

func (s *Store) undo(ctx context.Context, cwd, session string, turns int, dry bool) (UndoResult, error) {
	if turns < 1 {
		return UndoResult{}, fmt.Errorf("snapshot: turns must be at least 1, got %d", turns)
	}
	if err := checkCWD(cwd); err != nil {
		return UndoResult{}, err
	}
	if err := checkSession(session); err != nil {
		return UndoResult{}, err
	}
	if _, err := repoIgnores(ctx, cwd); err != nil {
		return UndoResult{}, err
	}
	dir := s.dirFor(cwd)
	defer s.lock(dir)()
	entries, err := readLedger(dir)
	if err != nil {
		return UndoResult{}, err
	}
	p := fold(entries, session)
	if p.cursor < 1 {
		return UndoResult{}, ErrNothingToUndo
	}
	if turns > p.cursor {
		return UndoResult{}, fmt.Errorf("%w: only %d turn(s) can be undone", ErrNothingToUndo, p.cursor)
	}
	target := p.cursor - turns + 1
	to, ok := p.starts[target]
	if !ok || to == "" {
		return UndoResult{}, ErrNotUndoable
	}
	if dry {
		return s.planTo(ctx, dir, cwd, session, to)
	}
	safety, err := s.capture(ctx, dir, cwd, session, PhaseSafety)
	if err != nil {
		return UndoResult{}, err
	}
	res, err := s.restore(ctx, dir, cwd, to, RestoreOpts{Session: session, From: safety})
	if err != nil {
		return UndoResult{}, err
	}
	err = appendLedger(dir, Entry{SessionID: session, Turn: target - 1, Phase: phaseUndo, Tree: to, At: s.now().UTC()})
	if err != nil {
		return UndoResult{}, err
	}
	return UndoResult{Restored: res.Restored, Deleted: res.Deleted, Safety: safety}, nil
}

// Redo brings back the state that the latest Undo replaced. History after a
// new turn started is gone: Redo then fails with ErrNothingToRedo.
func (s *Store) Redo(ctx context.Context, cwd, session string) (UndoResult, error) {
	return s.redo(ctx, cwd, session, false)
}

// PlanRedo is Redo without doing it, as PlanUndo is to Undo.
func (s *Store) PlanRedo(ctx context.Context, cwd, session string) (UndoResult, error) {
	return s.redo(ctx, cwd, session, true)
}

func (s *Store) redo(ctx context.Context, cwd, session string, dry bool) (UndoResult, error) {
	if err := checkCWD(cwd); err != nil {
		return UndoResult{}, err
	}
	if err := checkSession(session); err != nil {
		return UndoResult{}, err
	}
	if _, err := repoIgnores(ctx, cwd); err != nil {
		return UndoResult{}, err
	}
	dir := s.dirFor(cwd)
	defer s.lock(dir)()
	entries, err := readLedger(dir)
	if err != nil {
		return UndoResult{}, err
	}
	p := fold(entries, session)
	if len(p.undone) == 0 {
		return UndoResult{}, ErrNothingToRedo
	}
	top := p.undone[len(p.undone)-1]
	if dry {
		return s.planTo(ctx, dir, cwd, session, top.Tree)
	}
	cur, err := s.capture(ctx, dir, cwd, session, PhasePreRedo)
	if err != nil {
		return UndoResult{}, err
	}
	res, err := s.restore(ctx, dir, cwd, top.Tree, RestoreOpts{Session: session, From: cur})
	if err != nil {
		return UndoResult{}, err
	}
	err = appendLedger(dir, Entry{SessionID: session, Turn: top.Turn, Phase: phaseRedo, Tree: top.Tree, At: s.now().UTC()})
	if err != nil {
		return UndoResult{}, err
	}
	return UndoResult{Restored: res.Restored, Deleted: res.Deleted, Safety: cur}, nil
}

// MarkRemoved records that session left the board, which starts the clock
// for its retention. It is a no-op for a folder without a store.
func (s *Store) MarkRemoved(cwd, session string) error {
	if err := checkCWD(cwd); err != nil {
		return err
	}
	if err := checkSession(session); err != nil {
		return err
	}
	dir := s.dirFor(cwd)
	defer s.lock(dir)()
	if _, err := os.Stat(ledgerOf(dir)); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return appendLedger(dir, Entry{SessionID: session, Phase: phaseRemoved, At: s.now().UTC()})
}

// MarkMissed records that session's turn start could not be captured, so
// that turn is reported as not undoable instead of an earlier turn being
// undone in its place.
func (s *Store) MarkMissed(cwd, session string) error {
	if err := checkCWD(cwd); err != nil {
		return err
	}
	if err := checkSession(session); err != nil {
		return err
	}
	dir := s.dirFor(cwd)
	defer s.lock(dir)()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	entries, err := readLedger(dir)
	if err != nil {
		return err
	}
	p := fold(entries, session)
	return appendLedger(dir, Entry{SessionID: session, Turn: p.cursor + 1, Phase: PhaseStart, At: s.now().UTC()})
}

// planTo lists what restoring to would write and delete from the folder's
// current state. The current state is written as a tree that no ref keeps.
func (s *Store) planTo(ctx context.Context, dir, cwd, session string, to Tree) (UndoResult, error) {
	cur, env, err := s.writeTree(ctx, dir, cwd, session)
	if err != nil {
		return UndoResult{}, err
	}
	write, del, err := s.plan(ctx, env, cwd, cur, to)
	if err != nil {
		return UndoResult{}, err
	}
	return UndoResult{Restored: write, Deleted: del}, nil
}
