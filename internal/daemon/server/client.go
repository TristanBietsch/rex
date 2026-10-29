package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"time"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func handleClient(ctx context.Context, conn net.Conn, srv *Server) {
	cfg := srv.cfg
	// Every event to this client goes through c's queue: store broadcasts and
	// PTY output never block on this socket.
	c := newClient(conn)
	defer c.finish()
	r := protocol.NewLimitedReader(conn, protocol.MaxIntentLineBytes)

	// Board events: store changes and summarizer health. Subscribed for the
	// whole connection; c gates them on Hello (see client.hello).
	cancel := cfg.Store.Subscribe(c.sendStoreEvent)
	defer cancel()
	healthCancel := srv.SubscribeSummarizerHealth(func(h protocol.SummarizerHealth) {
		c.sendBoard(protocol.EventSummarizerHealth, h, "")
	})
	defer healthCancel()

	// outputs holds this connection's PTY output streams by session id.
	outputs := make(map[string]func())
	defer func() {
		for _, stop := range outputs {
			stop()
		}
	}()

	for {
		if ctx.Err() != nil {
			return
		}
		env, err := r.Read()
		if err != nil {
			// A line that arrived but can't be used ends the connection
			// (after an oversize one the stream is out of sync); say why.
			if errors.Is(err, protocol.ErrBadEnvelope) || errors.Is(err, protocol.ErrUnsupportedVersion) ||
				errors.Is(err, protocol.ErrLineTooLong) {
				slog.Warn("server: closing connection after unreadable line", "err", err)
				c.sendError("", protocol.ErrCodeBadIntent, err.Error())
			}
			return
		}
		if env.Kind != protocol.KindIntent {
			c.sendError(env.ID, protocol.ErrCodeBadIntent, "expected an Intent")
			continue
		}
		switch env.Type {
		case protocol.IntentHello:
			c.hello(env.ID, func() protocol.Snapshot {
				return protocol.Snapshot{Sessions: cfg.Store.Snapshot(), Filter: "all"}
			})
		case protocol.IntentNewSession:
			var p protocol.NewSession
			if err := json.Unmarshal(env.Data, &p); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := handleNewSession(ctx, env.ID, p, srv); err != nil {
				code := protocol.ErrCodeSpawn
				switch {
				case isBadIntent(err):
					code = protocol.ErrCodeBadIntent
				case errors.Is(err, errTooManySessions):
					code = protocol.ErrCodeTooManySessions
				}
				c.sendError(env.ID, code, err.Error())
			}
		case protocol.IntentDelete:
			var p protocol.Delete
			if err := json.Unmarshal(env.Data, &p); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := requireSessionID(p.SessionID); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			// Stops a running supervisor first so its goroutines don't race with
			// the on-disk cleanup; disk cleanup is best-effort.
			if err := srv.deleteSession(p.SessionID, protocol.RemovedByUser); err != nil {
				c.sendError(env.ID, protocol.ErrCodeUnknownSession, err.Error())
				continue
			}
		case protocol.IntentComplete:
			var p protocol.Complete
			if err := json.Unmarshal(env.Data, &p); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := requireSessionID(p.SessionID); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if !srv.CompleteSession(p.SessionID) {
				c.sendNotRunning(srv, env.ID, p.SessionID)
			}
		case protocol.IntentSubscribe:
			var p protocol.Subscribe
			if err := json.Unmarshal(env.Data, &p); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if p.SessionID == "" {
				continue // board-wide updates only: nothing to add
			}
			if err := requireSessionID(p.SessionID); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			// Only sessions the store knows: the id names a transcript on
			// disk and an output subscription that lives as long as the
			// connection.
			if _, ok := cfg.Store.Get(p.SessionID); !ok {
				c.sendError(env.ID, protocol.ErrCodeUnknownSession, "session not found")
				continue
			}
			// A repeat Subscribe replaces the stream instead of doubling it.
			if stop := outputs[p.SessionID]; stop != nil {
				stop()
			}
			outputs[p.SessionID] = srv.streamOutput(c, p)

		case protocol.IntentResize:
			var p protocol.Resize
			if err := json.Unmarshal(env.Data, &p); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := requireSessionID(p.SessionID); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if p.Cols == 0 || p.Rows == 0 {
				continue
			}
			if err := srv.Resize(p.SessionID, p.Cols, p.Rows); err != nil {
				c.sendError(env.ID, protocol.ErrCodeUnknownSession, err.Error())
			}

		case protocol.IntentSendInput:
			var p protocol.SendInput
			if err := json.Unmarshal(env.Data, &p); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := requireSessionID(p.SessionID); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			in := srv.input(p.SessionID)
			if in == nil {
				c.sendNotRunning(srv, env.ID, p.SessionID)
				continue
			}
			in.lock()
			sent := in.trySend(p.Bytes)
			in.mu.Unlock()
			if !sent {
				c.sendError(env.ID, protocol.ErrCodeBusy, "input buffer full")
				continue
			}
			if !p.Synthetic {
				srv.cancelRetireOnInput(p.SessionID, "SendInput")
			}

		case protocol.IntentReply:
			var p protocol.Reply
			if err := json.Unmarshal(env.Data, &p); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := requireSessionID(p.SessionID); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			in := srv.input(p.SessionID)
			if in == nil {
				c.sendNotRunning(srv, env.ID, p.SessionID)
				continue
			}
			// Reply is text to type, not raw keys: no escapes, no early
			// submit. The Enter follows on a timer, so the read loop goes on.
			text := sanitizeReplyText(p.Text)
			id, reqID := p.SessionID, env.ID
			queued := in.reply(text, func(ok bool) {
				if !ok {
					c.sendError(reqID, protocol.ErrCodeBusy, "input buffer full")
					return
				}
				slog.Debug("server: reply submitted", "session", id, "bytes", len(text))
				srv.cancelRetireOnInput(id, "Reply")
			})
			if !queued {
				c.sendError(env.ID, protocol.ErrCodeBusy, "input buffer full")
			}

		case protocol.IntentRename:
			var p protocol.Rename
			if err := json.Unmarshal(env.Data, &p); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := requireSessionID(p.SessionID); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			p.Title = normalizeTitle(p.Title)
			if err := validateRename(p); err != nil {
				slog.Warn("server: rename rejected", "session", p.SessionID, "slug", p.Slug, "reason", err, "req_id", env.ID)
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			sess, ok := srv.cfg.Store.Get(p.SessionID)
			if !ok {
				slog.Warn("server: rename rejected", "session", p.SessionID, "reason", "unknown session", "req_id", env.ID)
				c.sendError(env.ID, protocol.ErrCodeUnknownSession, "session not found")
				continue
			}
			if err := srv.cfg.Store.UpdateRename(p.SessionID, p.Slug, p.Title); err != nil {
				var taken *state.SlugTakenError
				if errors.As(err, &taken) {
					slog.Warn("server: rename rejected, slug taken", "session", p.SessionID,
						"slug", p.Slug, "holder", taken.ShortID, "req_id", env.ID)
					c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
					continue
				}
				slog.Warn("server: rename failed", "session", p.SessionID, "err", err, "req_id", env.ID)
				c.sendError(env.ID, protocol.ErrCodeUnknownSession, err.Error())
				continue
			}
			if err := state.WriteMeta(srv.cfg.StateDir, sess); err != nil {
				slog.Warn("server: write meta after rename", "session", p.SessionID, "err", err)
			}
			slog.Info("server: rename", "session", p.SessionID, "slug", p.Slug, "title_len", len(p.Title), "req_id", env.ID)

		case protocol.IntentSetSessionName:
			var p protocol.SetSessionName
			if err := json.Unmarshal(env.Data, &p); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := requireSessionID(p.SessionID); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			name, err := protocol.NormalizeSessionName(p.Name)
			if err != nil {
				slog.Warn("server: set_name rejected", "session", p.SessionID, "reason", err, "req_id", env.ID)
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			sess, ok := srv.cfg.Store.Get(p.SessionID)
			if !ok {
				slog.Warn("server: set_name rejected", "session", p.SessionID, "reason", "unknown session", "req_id", env.ID)
				c.sendError(env.ID, protocol.ErrCodeUnknownSession, "session not found")
				continue
			}
			changed, err := srv.cfg.Store.SetName(p.SessionID, name)
			if err != nil {
				c.sendError(env.ID, protocol.ErrCodeUnknownSession, err.Error())
				continue
			}
			// Broadcast even when unchanged: it is the confirmation clients wait on.
			srv.commit(sess, map[string]any{"name": name})
			slog.Info("server: set_name", "session", p.SessionID, "cleared", name == "",
				"runes", len([]rune(name)), "changed", changed, "req_id", env.ID)

		case protocol.IntentRetire:
			var p protocol.Retire
			if err := json.Unmarshal(env.Data, &p); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := requireSessionID(p.SessionID); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			at, ttl, err := srv.retireDeadline(p.TTLSeconds)
			if err != nil {
				slog.Warn("retire: invalid ttl", "session", p.SessionID, "ttl_s", p.TTLSeconds, "err", err)
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			sess, ok := srv.cfg.Store.Get(p.SessionID)
			if !ok {
				c.sendError(env.ID, protocol.ErrCodeUnknownSession, "session not found")
				continue
			}
			markedAt := srv.now().Round(0).UTC()
			changed, err := srv.cfg.Store.SetRetire(p.SessionID, at, markedAt)
			if err != nil {
				c.sendError(env.ID, protocol.ErrCodeUnknownSession, err.Error())
				continue
			}
			if !changed {
				slog.Debug("retire: already marked; no-op", "session", p.SessionID)
				continue
			}
			srv.commit(sess, map[string]any{"retire_at": at, "retired_at": markedAt})
			sum := sess.Summary()
			slog.Info("retire: marked", "session", p.SessionID, "slug", sum.Slug, "state", sum.State,
				"ttl_s", int64(ttl.Seconds()), "retire_at", at, "retired_at", markedAt)

		case protocol.IntentUnretire:
			var p protocol.Unretire
			if err := json.Unmarshal(env.Data, &p); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := requireSessionID(p.SessionID); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			sess, ok := srv.cfg.Store.Get(p.SessionID)
			if !ok {
				c.sendError(env.ID, protocol.ErrCodeUnknownSession, "session not found")
				continue
			}
			remaining := time.Until(sess.Summary().RetireAt)
			changed, err := srv.cfg.Store.ClearRetire(p.SessionID)
			if errors.Is(err, state.ErrReaping) {
				slog.Warn("retire: unmark rejected; clearing", "session", p.SessionID)
				c.sendError(env.ID, protocol.ErrCodeBadState, err.Error())
				continue
			}
			if err != nil {
				c.sendError(env.ID, protocol.ErrCodeUnknownSession, err.Error())
				continue
			}
			if changed {
				srv.commit(sess, map[string]any{"retire_at": nil, "retired_at": nil})
				slog.Info("retire: unmarked", "session", p.SessionID, "cause", "user", "remaining_s", int64(remaining.Seconds()))
			}

		case protocol.IntentSetSessionFleet:
			var p protocol.SetSessionFleet
			if err := json.Unmarshal(env.Data, &p); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := requireSessionID(p.SessionID); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			fleet, err := normalizeFleet(p.Fleet)
			if err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			p.Fleet = fleet
			sess, ok := srv.cfg.Store.Get(p.SessionID)
			if !ok {
				c.sendError(env.ID, protocol.ErrCodeUnknownSession, "session not found")
				continue
			}
			if err := srv.cfg.Store.SetFleet(p.SessionID, p.Fleet); err != nil {
				c.sendError(env.ID, protocol.ErrCodeUnknownSession, err.Error())
				continue
			}
			if err := state.WriteMeta(srv.cfg.StateDir, sess); err != nil {
				slog.Warn("server: write meta after fleet update", "session", p.SessionID, "err", err)
			}
			slog.Info("server: fleet updated", "session", p.SessionID, "fleet", p.Fleet)

		case protocol.IntentFocusFilter:
			var p protocol.FocusFilter
			_ = json.Unmarshal(env.Data, &p)
			// Cosmetic — silently accept.

		case protocol.IntentSetMaxConcurrent:
			var p protocol.SetMaxConcurrent
			if err := json.Unmarshal(env.Data, &p); err != nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			// The settings page offers 1..64; out-of-range values (n <= 0
			// would remove the cap) are clamped.
			n := min(max(p.N, minMaxConcurrent), maxMaxConcurrent)
			if n != p.N {
				slog.Warn("server: max concurrent sessions clamped", "requested", p.N, "applied", n)
			}
			srv.SetMaxConcurrentSessions(n)

		// Shutdown and Reload are the socket forms of SIGTERM and SIGHUP, for
		// platforms (Windows) that cannot signal the daemon.
		case protocol.IntentShutdown:
			if cfg.OnShutdown == nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, "intent not implemented")
				continue
			}
			slog.Info("server: shutdown requested by client")
			cfg.OnShutdown()

		case protocol.IntentReload:
			if cfg.OnReload == nil {
				c.sendError(env.ID, protocol.ErrCodeBadIntent, "intent not implemented")
				continue
			}
			if err := cfg.OnReload(); err != nil {
				c.sendError(env.ID, protocol.ErrCodeRegistry, err.Error())
			}

		default:
			c.sendError(env.ID, protocol.ErrCodeBadIntent, "intent not implemented")
		}
	}
}

// Bounds for the SetMaxConcurrent intent (the settings page's range).
const (
	minMaxConcurrent = 1
	maxMaxConcurrent = 64
)

// sendNotRunning answers an intent that needs a live PTY: unknown_session
// when the store has no such session, bad_state when it exists but its
// process has exited.
func (c *client) sendNotRunning(srv *Server, reqID, sessionID string) {
	if _, ok := srv.cfg.Store.Get(sessionID); !ok {
		c.sendError(reqID, protocol.ErrCodeUnknownSession, "session not found")
		return
	}
	c.sendError(reqID, protocol.ErrCodeBadState, "session not running")
}
