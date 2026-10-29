package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// DefaultRetireTTL is how long a marked-done session lingers before it is
// cleared when neither the intent nor the daemon config says otherwise.
const DefaultRetireTTL = 7 * 24 * time.Hour

// Retire TTL bounds accepted on the wire.
const (
	minRetireTTL = 10 * time.Second
	maxRetireTTL = 365 * 24 * time.Hour
)

const defaultRetireSweepInterval = 2 * time.Second

// now is the server clock (injectable through Config.Now).
func (s *Server) now() time.Time { return s.cfg.Now() }

// retireDeadline resolves a Retire intent's ttl into a wall-clock deadline.
// The monotonic reading is stripped so the comparison in the sweep uses wall
// time: a laptop that slept past the deadline clears on its first sweep.
func (s *Server) retireDeadline(ttlSeconds int64) (time.Time, time.Duration, error) {
	ttl := s.cfg.RetireDefaultTTL
	if ttlSeconds != 0 {
		ttl = time.Duration(ttlSeconds) * time.Second
		if ttl < minRetireTTL || ttl > maxRetireTTL {
			return time.Time{}, 0, fmt.Errorf("ttl_seconds out of range (%s..%s)", minRetireTTL, maxRetireTTL)
		}
	}
	return s.now().Add(ttl).Round(0).UTC(), ttl, nil
}

// deleteSession is the single teardown path for Delete and the retire sweep:
// stop the supervisor (blocks until it exits), drop the session from the
// store (broadcasts SessionRemoved with reason), delete its directory. A
// session that is already gone returns errSessionNotFound without side
// effects, so racing deletes are safe: the sweep ignores it, Delete reports
// unknown_session.
func (s *Server) deleteSession(id, reason string) error {
	start := time.Now()
	sess, ok := s.cfg.Store.Get(id)
	if !ok {
		slog.Debug("session: delete not found (idempotent)", "session", id, "reason", reason)
		return errSessionNotFound
	}
	live := s.hasStop(id)
	slog.Info("session: delete", "session", id, "slug", sess.Summary().Slug, "reason", reason, "live", live)
	s.StopSession(id)
	if err := s.cfg.Store.RemoveWithReason(id, reason); err != nil {
		slog.Debug("session: delete raced; already removed", "session", id, "reason", reason)
		return errSessionNotFound
	}
	if err := state.RemoveSession(s.cfg.StateDir, sess); err != nil {
		// The session is already gone from memory; on restart its meta (if it
		// survived) reloads and, for a retired session, is swept again.
		slog.Error("session: remove dir failed", "session", id, "err", err)
	}
	s.dropOutputSubs(id)
	slog.Info("session: deleted", "session", id, "reason", reason, "took_ms", time.Since(start).Milliseconds())
	return nil
}

var errSessionNotFound = errors.New("session not found")

// runRetireReaper sweeps once immediately (picking up deadlines restored from
// disk, including ones that passed while the daemon was down) and then on a
// ticker until ctx is canceled.
func (s *Server) runRetireReaper(ctx context.Context) {
	slog.Info("retire: reaper started", "interval", s.cfg.RetireSweepInterval, "default_ttl", s.cfg.RetireDefaultTTL)
	s.logRestoredRetires()
	s.sweepRetired()
	t := time.NewTicker(s.cfg.RetireSweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("retire: reaper stopped")
			return
		case <-t.C:
			s.sweepRetired()
		}
	}
}

func (s *Server) logRestoredRetires() {
	now := s.now()
	count, overdue := 0, 0
	for _, sum := range s.cfg.Store.Snapshot() {
		if sum.RetireAt.IsZero() {
			continue
		}
		count++
		if !sum.RetireAt.After(now) {
			overdue++
		}
	}
	if count > 0 {
		slog.Info("retire: restored from disk", "count", count, "overdue", overdue)
	}
}

// sweepRetired clears every expired, idle marked session. Busy (working)
// sessions wait; the reaper re-checks them every tick and clears them as soon
// as they settle. Deletes run serially — each blocks on supervisor exit.
func (s *Server) sweepRetired() {
	now := s.now()
	claimed, waiting := s.cfg.Store.ClaimExpired(now)
	s.noteRetireWaiting(waiting, now)
	if len(claimed) == 0 {
		return
	}
	slog.Debug("retire: sweep", "claimed", len(claimed), "waiting", len(waiting))
	for _, c := range claimed {
		slog.Info("retire: expired; clearing", "session", c.ID, "state", c.State,
			"retire_at", c.RetireAt, "overdue_ms", now.Sub(c.RetireAt).Milliseconds())
		_ = s.deleteSession(c.ID, protocol.RemovedRetired)
	}
}

// noteRetireWaiting logs once per busy stretch that an expired session is
// held back because it is still working. Sessions that stopped waiting
// (settled, unmarked, deleted) are forgotten so a later stretch logs again.
func (s *Server) noteRetireWaiting(waiting []state.RetireCandidate, now time.Time) {
	s.retireWaitMu.Lock()
	defer s.retireWaitMu.Unlock()
	next := make(map[string]bool, len(waiting))
	for _, c := range waiting {
		next[c.ID] = true
		if s.retireWaitLogged[c.ID] {
			continue
		}
		slog.Info("retire: expired but busy; waiting until it settles", "session", c.ID,
			"state", c.State, "overdue_ms", now.Sub(c.RetireAt).Milliseconds())
	}
	s.retireWaitLogged = next
}

// cancelRetireOnInput clears a pending retire when a user actually types into
// the session — using it again means it isn't done. Viewing does not count.
func (s *Server) cancelRetireOnInput(id, via string) {
	sess, ok := s.cfg.Store.Get(id)
	if !ok {
		return
	}
	if sess.Summary().RetireAt.IsZero() {
		return
	}
	changed, err := s.cfg.Store.ClearRetire(id)
	if err != nil {
		slog.Warn("retire: input after claim; clearing proceeds", "session", id, "via", via, "err", err)
		return
	}
	if !changed {
		return
	}
	slog.Info("retire: unmarked", "session", id, "cause", "input", "via", via)
	s.commit(sess, map[string]any{"retire_at": nil, "retired_at": nil})
}

// commit persists a session's meta and then broadcasts patch, so a client
// reacting to the event finds the change already on disk. A failed write is
// logged and the broadcast still goes out: memory is the source of truth for
// this daemon's lifetime.
func (s *Server) commit(sess *state.Session, patch map[string]any) {
	if err := state.WriteMeta(s.cfg.StateDir, sess); err != nil {
		slog.Error("server: persist meta failed", "session", sess.ID, "err", err)
	}
	s.cfg.Store.BroadcastPatch(sess.ID, patch)
}
