package server

import (
	"context"
	"errors"
	"fmt"
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
	return c.srv.undoRedo(c, env, p.SessionID, p.Force, func(ctx context.Context, cwd string) (snapshot.UndoResult, error) {
		return c.srv.cfg.Snapshots.Undo(ctx, cwd, p.SessionID, turns)
	})
}

func handleRedo(c *client, env protocol.Envelope) (outcome, error) {
	p, err := decodeSessionIntent(env, func(p protocol.Redo) string { return p.SessionID })
	if err != nil {
		return outcome{}, err
	}
	return c.srv.undoRedo(c, env, p.SessionID, p.Force, func(ctx context.Context, cwd string) (snapshot.UndoResult, error) {
		return c.srv.cfg.Snapshots.Redo(ctx, cwd, p.SessionID)
	})
}

// undoRedo runs the checks Undo and Redo share, then do, and sends the
// UndoResult.
func (s *Server) undoRedo(c *client, env protocol.Envelope, id string, force bool,
	do func(ctx context.Context, cwd string) (snapshot.UndoResult, error)) (outcome, error) {
	if !s.snapshotsOn() {
		return outcome{}, replyMsg(protocol.ErrCodeBadIntent, msgSnapshotsOff)
	}
	sess, ok := s.cfg.Store.Get(id)
	if !ok {
		return outcome{}, replyMsg(protocol.ErrCodeUnknownSession, "session not found")
	}
	sum := sess.Summary()
	if sum.State == protocol.StateWorking {
		return outcome{}, replyMsg(protocol.ErrCodeBadState,
			"session is working: wait until the agent stops, or press esc to interrupt it first")
	}
	if others := s.liveInFolder(sum); len(others) > 0 && !force {
		return outcome{}, replyMsg(protocol.ErrCodeBadState, fmt.Sprintf(
			"another live session works in this folder (%s); its changes would be reverted too. Use force to go ahead",
			strings.Join(others, ", ")))
	}
	ctx, cancel := context.WithTimeout(c.ctx, undoTimeout)
	defer cancel()
	res, err := do(ctx, sum.CWD)
	if err != nil {
		return outcome{}, replyErr(protocol.ErrCodeBadState, undoError(err))
	}
	if env.ID != "" {
		c.sendEvent(protocol.EventUndoResult, env.ID, protocol.UndoResult{
			SessionID: id,
			Restored:  nonNil(res.Restored),
			Deleted:   nonNil(res.Deleted),
			Safety:    string(res.Safety),
		})
	}
	return outcome{sessionID: id, replied: true}, nil
}

// liveInFolder lists the slugs of the other live (not done, failed or
// crashed) sessions that share sum's folder.
func (s *Server) liveInFolder(sum protocol.SessionSummary) []string {
	var out []string
	for _, o := range s.cfg.Store.Snapshot() {
		if o.ID != sum.ID && o.CWD == sum.CWD && !state.IsTerminal(o.State) {
			out = append(out, o.Slug)
		}
	}
	slices.Sort(out)
	return out
}

// undoError keeps the snapshot package's own wording for the user, and
// names the folder rule for a non-repo folder.
func undoError(err error) error {
	switch {
	case errors.Is(err, snapshot.ErrNotGitWorktree), errors.Is(err, snapshot.ErrNothingToUndo),
		errors.Is(err, snapshot.ErrNothingToRedo), errors.Is(err, snapshot.ErrNotUndoable),
		errors.Is(err, snapshot.ErrNoGit):
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
