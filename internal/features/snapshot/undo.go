package snapshot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
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
	// Warnings are things the caller should show: the folder changed while
	// the restore ran, or the guard tripped after it.
	Warnings []string
}

// Guard is checked after the safety capture and before the first file is
// touched (and once more after the restore). An error aborts the operation
// before anything changes; after the restore it becomes a warning.
type Guard func() error

type opts struct{ guard Guard }

// Option adjusts Undo, Redo and their Plan forms.
type Option func(*opts)

// WithGuard sets the Guard.
func WithGuard(g Guard) Option { return func(o *opts) { o.guard = g } }

func collect(os []Option) opts {
	var o opts
	for _, f := range os {
		f(&o)
	}
	return o
}

// Undo restores cwd to how it was before session's last turns turns, after a
// safety capture of the current state. Repeated calls walk further back.
// The folder's lock is held from the safety capture through the restore, so
// the daemon's own captures wait. The caller checks that the session is not
// working and that no other live session shares the folder, and passes a
// Guard that checks it again at the last moment.
func (s *Store) Undo(ctx context.Context, cwd, session string, turns int, o ...Option) (UndoResult, error) {
	return s.undo(ctx, cwd, session, turns, false, collect(o))
}

// PlanUndo is Undo without doing it: it lists the files Undo would write and
// delete and changes nothing in the folder or its history (Safety is empty).
// It refuses what Undo would refuse, including a *BlockedError.
func (s *Store) PlanUndo(ctx context.Context, cwd, session string, turns int) (UndoResult, error) {
	return s.undo(ctx, cwd, session, turns, true, opts{})
}

func (s *Store) undo(ctx context.Context, cwd, session string, turns int, dry bool, o opts) (UndoResult, error) {
	if turns < 1 {
		return UndoResult{}, fmt.Errorf("snapshot: turns must be at least 1, got %d", turns)
	}
	dir, unlock, err := s.begin(ctx, cwd, session)
	if err != nil {
		return UndoResult{}, err
	}
	defer unlock()
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
	res, err := s.apply(ctx, dir, cwd, session, to, PhaseSafety, dry, o)
	if err != nil || dry {
		return res, err
	}
	err = appendLedger(dir, Entry{SessionID: session, Turn: target - 1, Phase: phaseUndo, Tree: to, At: s.now().UTC()})
	return res, err
}

// Redo brings back the state that the latest Undo replaced. History after a
// new turn started is gone: Redo then fails with ErrNothingToRedo.
func (s *Store) Redo(ctx context.Context, cwd, session string, o ...Option) (UndoResult, error) {
	return s.redo(ctx, cwd, session, false, collect(o))
}

// PlanRedo is Redo without doing it, as PlanUndo is to Undo.
func (s *Store) PlanRedo(ctx context.Context, cwd, session string) (UndoResult, error) {
	return s.redo(ctx, cwd, session, true, opts{})
}

func (s *Store) redo(ctx context.Context, cwd, session string, dry bool, o opts) (UndoResult, error) {
	dir, unlock, err := s.begin(ctx, cwd, session)
	if err != nil {
		return UndoResult{}, err
	}
	defer unlock()
	entries, err := readLedger(dir)
	if err != nil {
		return UndoResult{}, err
	}
	p := fold(entries, session)
	if len(p.undone) == 0 {
		return UndoResult{}, ErrNothingToRedo
	}
	top := p.undone[len(p.undone)-1]
	res, err := s.apply(ctx, dir, cwd, session, top.Tree, PhasePreRedo, dry, o)
	if err != nil || dry {
		return res, err
	}
	err = appendLedger(dir, Entry{SessionID: session, Turn: top.Turn, Phase: phaseRedo, Tree: top.Tree, At: s.now().UTC()})
	return res, err
}

// begin validates the arguments, checks the folder is a git worktree and
// takes the folder lock.
func (s *Store) begin(ctx context.Context, cwd, session string) (dir string, unlock func(), err error) {
	if err := checkCWD(cwd); err != nil {
		return "", nil, err
	}
	if err := checkSession(session); err != nil {
		return "", nil, err
	}
	if _, err := repoIgnores(ctx, cwd); err != nil {
		return "", nil, err
	}
	dir = s.dirFor(cwd)
	unlock, err = s.lockCtx(ctx, dir)
	return dir, unlock, err
}

// apply restores the folder to the tree to, with the folder lock held. It
// plans first (refusing, with nothing recorded, what cannot be done safely),
// then takes the safety capture, checks the guard, restores, and checks that
// the folder ended up as the target.
func (s *Store) apply(ctx context.Context, dir, cwd, session string, to Tree, safetyPhase Phase, dry bool, o opts) (UndoResult, error) {
	plan, err := s.planTo(ctx, dir, cwd, session, to)
	if err != nil || dry {
		return plan, err
	}
	entries, err := readLedger(dir)
	if err != nil {
		return UndoResult{}, err
	}
	cursor := fold(entries, session).cursor
	safety, err := s.capture(ctx, dir, cwd, session, safetyPhase)
	if err != nil {
		return UndoResult{}, err
	}
	if o.guard != nil {
		if gerr := o.guard(); gerr != nil {
			if safetyPhase == PhaseSafety {
				// Drop the unused safety capture from the redo stack.
				_ = appendLedger(dir, Entry{SessionID: session, Turn: cursor, Phase: phaseRedo, Tree: safety, At: s.now().UTC()})
			}
			return UndoResult{}, gerr
		}
	}
	res, err := s.restore(ctx, dir, cwd, to, RestoreOpts{Session: session, From: safety})
	if err != nil {
		var be *BlockedError
		if errors.As(err, &be) {
			if safetyPhase == PhaseSafety {
				_ = appendLedger(dir, Entry{SessionID: session, Turn: cursor, Phase: phaseRedo, Tree: safety, At: s.now().UTC()})
			}
			return UndoResult{}, err
		}
		return UndoResult{}, fmt.Errorf("%w; the folder may be half restored: its state before is saved, run rex redo to bring it back", err)
	}
	out := UndoResult{Restored: res.Restored, Deleted: res.Deleted, Safety: safety}
	out.Warnings = s.verify(ctx, dir, cwd, session, to, o)
	return out, nil
}

// verify captures the folder again and compares it to the target, leaving
// out ignored paths; and re-checks the guard. It returns warnings.
func (s *Store) verify(ctx context.Context, dir, cwd, session string, to Tree, o opts) []string {
	var warns []string
	post, env, err := s.writeTree(ctx, dir, cwd, session)
	if err != nil {
		warns = append(warns, "could not verify the restored folder: "+err.Error())
	} else if ch, err := s.changed(ctx, env, cwd, post, to); err != nil {
		warns = append(warns, "could not verify the restored folder: "+err.Error())
	} else if len(ch) > 0 {
		paths := make([]string, len(ch))
		for i, c := range ch {
			paths[i] = c.Path
		}
		if kept, err := s.dropIgnored(ctx, env, cwd, paths); err == nil {
			paths = kept
		}
		if len(paths) > 0 {
			slices.Sort(paths)
			n := len(paths)
			if n > 5 {
				paths = paths[:5]
			}
			warns = append(warns, fmt.Sprintf("%d file(s) differ from the snapshot after the restore (something changed the folder meanwhile), for example %v", n, paths))
		}
	}
	if o.guard != nil {
		if gerr := o.guard(); gerr != nil {
			warns = append(warns, "while the restore ran: "+gerr.Error())
		}
	}
	return warns
}

// MarkRemoved records that session left the board, which starts the clock
// for its retention. It is a no-op for a folder without a store.
func (s *Store) MarkRemoved(ctx context.Context, cwd, session string) error {
	if err := checkCWD(cwd); err != nil {
		return err
	}
	if err := checkSession(session); err != nil {
		return err
	}
	dir := s.dirFor(cwd)
	unlock, err := s.lockCtx(ctx, dir)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := os.Stat(ledgerOf(dir)); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return appendLedger(dir, Entry{SessionID: session, Phase: phaseRemoved, At: s.now().UTC()})
}

// MarkMissed records that session's turn start could not be captured, so
// that turn is reported as not undoable instead of an earlier turn being
// undone in its place.
func (s *Store) MarkMissed(ctx context.Context, cwd, session string) error {
	if err := checkCWD(cwd); err != nil {
		return err
	}
	if err := checkSession(session); err != nil {
		return err
	}
	dir := s.dirFor(cwd)
	unlock, err := s.lockCtx(ctx, dir)
	if err != nil {
		return err
	}
	defer unlock()
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
// current state, refusing a restore that preflight blocks. The current state
// is written as a tree that no ref keeps.
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
