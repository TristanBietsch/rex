package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// errResumeCancelled means a Delete removed the session while its resume was
// in flight; nothing was started.
var errResumeCancelled = errors.New("session was deleted while resuming")

// handleResume makes a session live and not marked done: a running session
// just loses its done mark, an exited one is respawned in place with the
// tool's resume arguments (same id, slug, row and transcript).
func handleResume(c *client, env protocol.Envelope) (outcome, error) {
	p, err := decodeSessionIntent(env, func(p protocol.Resume) string { return p.SessionID })
	if err != nil {
		return outcome{}, err
	}
	srv := c.srv
	if !srv.resumeOn() {
		return outcome{}, replyMsg(protocol.ErrCodeBadIntent, "resume is off")
	}
	sess, ok := srv.cfg.Store.Get(p.SessionID)
	if !ok {
		return outcome{}, replyMsg(protocol.ErrCodeUnknownSession, "session not found")
	}
	// Only an exited session with no supervisor is respawned. One that is
	// still starting (queued, no stop func yet) counts as live: touching its
	// hook file or stop slot now would race its own startup.
	if !srv.hasStop(p.SessionID) && state.IsTerminal(sess.Summary().State) {
		resumed, err := srv.resumeExited(c.ctx, sess)
		if err != nil {
			return outcome{}, err
		}
		if resumed {
			return outcome{sessionID: p.SessionID}, nil
		}
		// Lost the race for the stop slot: the session is live now.
	}
	if err := srv.unmarkLive(sess); err != nil {
		return outcome{}, err
	}
	return outcome{sessionID: p.SessionID}, nil
}

// unmarkLive clears a running session's done mark, the same as Unretire.
func (s *Server) unmarkLive(sess *state.Session) error {
	changed, err := s.cfg.Store.ClearRetire(sess.ID)
	if errors.Is(err, state.ErrReaping) {
		return replyErr(protocol.ErrCodeBadState, err)
	}
	if err != nil {
		return replyErr(protocol.ErrCodeUnknownSession, err)
	}
	if changed {
		s.commit(sess, map[string]any{"retire_at": nil, "retired_at": nil})
		slog.Info("resume: unmarked running session", "session", sess.ID)
	}
	return nil
}

// resumeExited respawns a done, failed or crashed session in place. It
// reports false, with nothing changed, when another resume or a supervisor
// already holds the session's stop slot.
func (s *Server) resumeExited(ctx context.Context, sess *state.Session) (bool, error) {
	cfg := s.cfg
	sum := sess.Summary()
	tool, model, ok := s.Registry().FindModel(sum.ToolID, sum.ModelID)
	if !ok {
		return false, replyErr(protocol.ErrCodeBadIntent, unknownToolError(sum.ToolID, sum.ModelID))
	}
	if sum.Effort != "" && model.Effort != nil && !slices.Contains(model.Effort.Options, sum.Effort) {
		return false, replyMsg(protocol.ErrCodeBadState, fmt.Sprintf("effort %q is no longer offered by %s/%s",
			sum.Effort, sum.ToolID, sum.ModelID))
	}
	if tool.Resume == nil || !protocol.ValidResumeID(sum.ResumeID) {
		return false, replyMsg(protocol.ErrCodeBadState, fmt.Sprintf("session exited and %s cannot be resumed", tool.ID))
	}
	if fi, err := os.Stat(sum.CWD); err != nil || !fi.IsDir() {
		return false, replyMsg(protocol.ErrCodeSpawn, fmt.Sprintf("working directory %s is gone", sum.CWD))
	}

	// Claim the stop slot first: of two concurrent Resumes exactly one wins.
	res, ok := s.reserveStop(sess.ID)
	if !ok {
		return false, nil
	}
	defer s.unreserveStop(sess.ID, res) // no-op once startSupervisor promoted it

	if !s.TryAcquireSession() {
		return false, replyMsg(protocol.ErrCodeTooManySessions,
			fmt.Sprintf("too many concurrent sessions (cap=%d)", s.MaxConcurrentSessions()))
	}
	acquired := true
	defer func() {
		if acquired {
			s.ReleaseSession()
		}
	}()

	// A stale hook file would replay the previous run's lines into the new
	// adapter. It never recreates a deleted session's directory.
	hookFile, err := state.HookFile(cfg.StateDir, sess.ID)
	if err != nil {
		return false, replyErr(protocol.ErrCodeSpawn, fmt.Errorf("hook file: %w", err))
	}
	if tool.Detect.Kind == "hooks" {
		if _, err := state.ResetHookFile(cfg.StateDir, sess.ID); err != nil {
			if _, ok := cfg.Store.Get(sess.ID); !ok {
				return false, replyMsg(protocol.ErrCodeUnknownSession, "session not found")
			}
			return false, replyErr(protocol.ErrCodeSpawn, fmt.Errorf("hook file: %w", err))
		}
	}

	// Skills load again: the snapshot staged at creation, or a fresh copy.
	pluginDir, err := s.resumePlugin(tool, sess.ID, sum.Skills)
	if err != nil {
		return false, replyMsg(protocol.ErrCodeBadState, fmt.Sprintf("skills for this session cannot be loaded: %v", err))
	}

	if err := cfg.Store.Reopen(sess.ID); err != nil {
		switch {
		case errors.Is(err, state.ErrReaping), errors.Is(err, state.ErrNotExited):
			return false, replyErr(protocol.ErrCodeBadState, err)
		default:
			return false, replyErr(protocol.ErrCodeUnknownSession, err)
		}
	}
	reopened := sess.Summary()
	s.commit(sess, map[string]any{
		"state":         reopened.State,
		"last_event_at": reopened.LastEventAt,
		"ended_at":      nil,
		"exit_code":     nil,
		"retire_at":     nil,
		"retired_at":    nil,
	})

	spec := spawnSpec{
		Tool:      tool,
		Model:     model,
		Effort:    sum.Effort,
		CWD:       sum.CWD,
		PluginDir: pluginDir,
		// Trust is recomputed from the current config and the session's own
		// trust request (re-validated against the stored folder).
		Trust:           s.trustDecisionFor(tool, sum.CWD, sum.TrustFolder),
		SkipPermissions: cfg.SkipPermissions != nil && cfg.SkipPermissions(),
		HookFile:        hookFile,
		SessionID:       sess.ID,
		Resume:          &resumeArgs{ID: sum.ResumeID},
	}
	argv := buildArgv(spec)
	if err := startSupervisor(ctx, s, sess, spec, argv, childEnvFor(spec), "", promptDelivery{}, res); err != nil {
		if errors.Is(err, errResumeCancelled) {
			slog.Info("resume: session deleted while resuming; nothing started", "session", sess.ID)
			return false, replyMsg(protocol.ErrCodeUnknownSession, "session not found")
		}
		// The row is queued with no supervisor: settle it as failed.
		slog.Error("resume: start failed", "session", sess.ID, "err", err)
		sess.SetExit(protocol.StateFailed, nil)
		if werr := state.WriteMeta(cfg.StateDir, sess); werr != nil {
			slog.Error("resume: persist failed state", "session", sess.ID, "err", werr)
		}
		_ = cfg.Store.Transition(sess.ID, protocol.StateFailed)
		return false, replyErr(protocol.ErrCodeSpawn, err)
	}
	// The supervisor goroutine now owns the slot (it releases in its defers).
	acquired = false
	slog.Info("resume: respawned", "session", sess.ID, "slug", sum.Slug, "tool", tool.ID, "resume_id", sum.ResumeID)
	return true, nil
}
