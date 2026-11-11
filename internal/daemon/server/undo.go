package server

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/features/snapshot"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// undoTimeout bounds one Undo or Redo: a safety capture plus a restore.
const undoTimeout = 2 * time.Minute

// maxUndoTurns bounds Undo.Turns.
const maxUndoTurns = 1000

// msgSnapshotsOff is the refusal while turn_snapshots is off.
const msgSnapshotsOff = "off: enable turn_snapshots in Settings → Features"

// snapshotsOn reports whether the turn_snapshots feature is on.
func (s *Server) snapshotsOn() bool {
	return s.cfg.Snapshots != nil && s.cfg.SnapshotsEnabled != nil && s.cfg.SnapshotsEnabled()
}

func handleUndo(c *client, env protocol.Envelope) (outcome, error) {
	p, err := decodeSessionIntent(env, func(p protocol.Undo) string { return p.SessionID })
	if err != nil {
		return outcome{}, err
	}
	if p.Turns < 0 || p.Turns > maxUndoTurns {
		return outcome{}, replyMsg(protocol.ErrCodeBadIntent, fmt.Sprintf("turns must be 1 to %d", maxUndoTurns))
	}
	turns := max(p.Turns, 1)
	st := c.srv.cfg.Snapshots
	return c.srv.undoRedo(c, env, p.SessionID, p.Force, func(ctx context.Context, cwd string, g snapshot.Guard) (snapshot.UndoResult, error) {
		if p.DryRun {
			return st.PlanUndo(ctx, cwd, p.SessionID, turns)
		}
		return st.Undo(ctx, cwd, p.SessionID, turns, snapshot.WithGuard(g))
	})
}

func handleRedo(c *client, env protocol.Envelope) (outcome, error) {
	p, err := decodeSessionIntent(env, func(p protocol.Redo) string { return p.SessionID })
	if err != nil {
		return outcome{}, err
	}
	st := c.srv.cfg.Snapshots
	return c.srv.undoRedo(c, env, p.SessionID, p.Force, func(ctx context.Context, cwd string, g snapshot.Guard) (snapshot.UndoResult, error) {
		if p.DryRun {
			return st.PlanRedo(ctx, cwd, p.SessionID)
		}
		return st.Redo(ctx, cwd, p.SessionID, snapshot.WithGuard(g))
	})
}

// undoRedo runs the checks Undo and Redo share, then do, and sends the
// UndoResult.
func (s *Server) undoRedo(c *client, env protocol.Envelope, id string, force bool,
	do func(ctx context.Context, cwd string, g snapshot.Guard) (snapshot.UndoResult, error)) (outcome, error) {
	if !s.snapshotsOn() {
		return outcome{}, replyMsg(protocol.ErrCodeBadIntent, msgSnapshotsOff)
	}
	sess, ok := s.cfg.Store.Get(id)
	if !ok {
		return outcome{}, replyMsg(protocol.ErrCodeUnknownSession, "session not found")
	}
	if err := s.undoRefusal(sess, force); err != nil {
		return outcome{}, err
	}
	sum := sess.Summary()
	// The restore must not die with the client's connection: a git call killed
	// half way would leave the folder half restored.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.ctx), undoTimeout)
	defer cancel()
	// Checked again after the safety capture, just before the first change.
	guard := func() error { return s.undoRefusal(sess, force) }
	res, err := do(ctx, sum.CWD, guard)
	if err != nil {
		return outcome{}, replyErr(protocol.ErrCodeBadState, undoError(err))
	}
	if env.ID != "" {
		c.sendEvent(protocol.EventUndoResult, env.ID, protocol.UndoResult{
			SessionID: id,
			Restored:  nonNil(res.Restored),
			Deleted:   nonNil(res.Deleted),
			Safety:    string(res.Safety),
			Warnings:  res.Warnings,
		})
	}
	return outcome{sessionID: id, replied: true}, nil
}

// undoRefusal is why sess may not be undone or redone right now, or nil: it
// is working, or another live session overlaps its folder (unless force).
func (s *Server) undoRefusal(sess *state.Session, force bool) error {
	sum := sess.Summary()
	if sum.State == protocol.StateWorking {
		return replyMsg(protocol.ErrCodeBadState,
			"session is working: wait until the agent stops, or press esc to interrupt it first")
	}
	if others := s.liveInFolder(sum); len(others) > 0 && !force {
		return replyMsg(protocol.ErrCodeBadState, fmt.Sprintf(
			"another live session works in this folder (%s); its changes would be reverted too. Use force to go ahead",
			strings.Join(others, ", ")))
	}
	return nil
}

// liveInFolder lists the slugs of the other live (not done, failed or
// crashed) sessions whose folder is sum's folder or inside or around it,
// comparing cleaned, symlink-resolved paths.
func (s *Server) liveInFolder(sum protocol.SessionSummary) []string {
	mine := canonicalDir(sum.CWD)
	var out []string
	for _, o := range s.cfg.Store.Snapshot() {
		if o.ID != sum.ID && !state.IsTerminal(o.State) && foldersOverlap(mine, canonicalDir(o.CWD)) {
			out = append(out, o.Slug)
		}
	}
	slices.Sort(out)
	return out
}

// canonicalDir cleans p and resolves its symlinks (as far as it exists);
// Windows paths compare case-insensitively.
func canonicalDir(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

// foldersOverlap reports whether a and b are the same folder or one is inside
// the other: a restore of either touches the other's files.
func foldersOverlap(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+string(filepath.Separator)) || strings.HasPrefix(b, a+string(filepath.Separator))
}

// undoError keeps the snapshot package's own wording for the user, and
// names the folder rule for a non-repo folder.
func undoError(err error) error {
	switch {
	case errors.Is(err, snapshot.ErrNotGitWorktree), errors.Is(err, snapshot.ErrNothingToUndo),
		errors.Is(err, snapshot.ErrNothingToRedo), errors.Is(err, snapshot.ErrNotUndoable),
		errors.Is(err, snapshot.ErrNoGit), errors.Is(err, snapshot.ErrGitTooOld),
		errors.Is(err, snapshot.ErrDubiousOwnership):
		return err
	}
	var blocked *snapshot.BlockedError
	if errors.As(err, &blocked) {
		return err
	}
	if strings.Contains(err.Error(), "rex redo") {
		return err
	}
	return fmt.Errorf("undo failed: %w", err)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
