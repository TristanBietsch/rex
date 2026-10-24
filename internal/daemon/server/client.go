package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// replySubmitDelay separates a Reply's text from its Enter keystroke so TUIs
// commit the text to their input box before submitting (matches the initial
// prompt paste path in pty.Supervisor).
const replySubmitDelay = 120 * time.Millisecond

func handleClient(ctx context.Context, conn net.Conn, srv *Server) {
	cfg := srv.cfg
	defer conn.Close()
	r := protocol.NewReader(conn)
	w := protocol.NewWriter(conn)

	// Per-client subscription that forwards store events back to this client.
	// Use atomic so the subscriber goroutine and the handler goroutine can read/write
	// the flag safely under the race detector.
	var subscribed atomic.Bool
	cancel := cfg.Store.Subscribe(func(e state.Event) {
		if !subscribed.Load() {
			return
		}
		emitEvent(w, e)
	})
	defer cancel()

	// Global SummarizerHealth subscription — gated on the same `subscribed` flag
	// so we don't emit events before the client has received its Snapshot.
	healthCancel := srv.SubscribeSummarizerHealth(func(h protocol.SummarizerHealth) {
		if !subscribed.Load() {
			return
		}
		writeEvent(w, protocol.EventSummarizerHealth, "", h)
	})
	defer healthCancel()

	for {
		if ctx.Err() != nil {
			return
		}
		env, err := r.Read()
		if err != nil {
			return
		}
		if env.Kind != protocol.KindIntent {
			writeError(w, env.ID, protocol.ErrCodeBadIntent, "expected an Intent")
			continue
		}
		switch env.Type {
		case protocol.IntentHello:
			snap := protocol.Snapshot{Sessions: cfg.Store.Snapshot(), Filter: "all"}
			writeEvent(w, protocol.EventSnapshot, env.ID, snap)
			subscribed.Store(true)
		case protocol.IntentNewSession:
			var p protocol.NewSession
			if err := json.Unmarshal(env.Data, &p); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := handleNewSession(ctx, env.ID, p, srv, w); err != nil {
				code := protocol.ErrCodeSpawn
				switch {
				case isBadIntent(err):
					code = protocol.ErrCodeBadIntent
				case strings.Contains(err.Error(), "too many concurrent sessions"):
					code = protocol.ErrCodeTooManySessions
				}
				writeError(w, env.ID, code, err.Error())
			}
		case protocol.IntentDelete:
			var p protocol.Delete
			if err := json.Unmarshal(env.Data, &p); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := requireSessionID(p.SessionID); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			// Stops a running supervisor first so its goroutines don't race with
			// the on-disk cleanup; disk cleanup is best-effort.
			if err := srv.deleteSession(p.SessionID, protocol.RemovedByUser); err != nil {
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, err.Error())
				continue
			}
		case protocol.IntentComplete:
			var p protocol.Complete
			if err := json.Unmarshal(env.Data, &p); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := requireSessionID(p.SessionID); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if _, ok := cfg.Store.Get(p.SessionID); !ok {
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, "session not found")
				continue
			}
			srv.CompleteSession(p.SessionID)
		case protocol.IntentSubscribe:
			var p protocol.Subscribe
			if err := json.Unmarshal(env.Data, &p); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if p.SessionID != "" {
				if err := requireSessionID(p.SessionID); err != nil {
					writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
					continue
				}
				var preambleBytes int
				if p.Replay {
					if tail, err := state.TranscriptTail(srv.TranscriptDir(), p.SessionID, transcriptReplayMax); err == nil && len(tail) > 0 {
						writeEvent(w, protocol.EventSessionOutput, "", protocol.SessionOutput{
							SessionID: p.SessionID, Bytes: tail,
						})
					}
				} else if p.ModeSync {
					// The replay already carries the child's mode switches;
					// without one, send them as a preamble.
					if pre := srv.ModePreamble(p.SessionID); len(pre) > 0 {
						preambleBytes = len(pre)
						writeEvent(w, protocol.EventSessionOutput, "", protocol.SessionOutput{
							SessionID: p.SessionID, Bytes: pre,
						})
					}
				}
				slog.Info("server: subscribe", "session", p.SessionID, "replay", p.Replay,
					"mode_sync", p.ModeSync, "preamble_bytes", preambleBytes)
				// Register an output sink for this session for the rest of the connection.
				outCancel := srv.SubscribeSessionOutput(p.SessionID, func(b []byte) {
					writeEvent(w, protocol.EventSessionOutput, "", protocol.SessionOutput{
						SessionID: p.SessionID, Bytes: b,
					})
				})
				defer outCancel()
			}

		case protocol.IntentResize:
			var p protocol.Resize
			if err := json.Unmarshal(env.Data, &p); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := requireSessionID(p.SessionID); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if p.Cols == 0 || p.Rows == 0 {
				continue
			}
			if err := srv.Resize(p.SessionID, p.Cols, p.Rows); err != nil {
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, err.Error())
			}

		case protocol.IntentSendInput:
			var p protocol.SendInput
			if err := json.Unmarshal(env.Data, &p); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := requireSessionID(p.SessionID); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			ch := srv.InputChannel(p.SessionID)
			if ch == nil {
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, "session not running")
				continue
			}
			select {
			case ch <- p.Bytes:
				if !p.Synthetic {
					srv.cancelRetireOnInput(p.SessionID, "SendInput")
				}
			default:
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, "input buffer full")
			}

		case protocol.IntentReply:
			var p protocol.Reply
			if err := json.Unmarshal(env.Data, &p); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := requireSessionID(p.SessionID); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			ch := srv.InputChannel(p.SessionID)
			if ch == nil {
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, "session not running")
				continue
			}
			// Text, then Enter as a separate write. Terminals send Enter as
			// \r (the PTY maps it to \n for line REPLs); full-screen TUIs like
			// Claude Code insert a newline on \n instead of submitting, and
			// treat text+\r in one burst as a paste.
			select {
			case ch <- []byte(p.Text):
			default:
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, "input buffer full")
				continue
			}
			time.Sleep(replySubmitDelay)
			select {
			case ch <- []byte{'\r'}:
				slog.Debug("server: reply submitted", "session", p.SessionID, "bytes", len(p.Text))
				srv.cancelRetireOnInput(p.SessionID, "Reply")
			default:
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, "input buffer full")
			}

		case protocol.IntentRename:
			var p protocol.Rename
			if err := json.Unmarshal(env.Data, &p); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := requireSessionID(p.SessionID); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := validateRename(p); err != nil {
				slog.Warn("server: rename rejected", "session", p.SessionID, "slug", p.Slug, "reason", err, "req_id", env.ID)
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			sess, ok := srv.cfg.Store.Get(p.SessionID)
			if !ok {
				slog.Warn("server: rename rejected", "session", p.SessionID, "reason", "unknown session", "req_id", env.ID)
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, "session not found")
				continue
			}
			if err := srv.cfg.Store.UpdateRename(p.SessionID, p.Slug, p.Title); err != nil {
				var taken *state.SlugTakenError
				if errors.As(err, &taken) {
					slog.Warn("server: rename rejected, slug taken", "session", p.SessionID,
						"slug", p.Slug, "holder", taken.ShortID, "req_id", env.ID)
					writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
					continue
				}
				slog.Warn("server: rename failed", "session", p.SessionID, "err", err, "req_id", env.ID)
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, err.Error())
				continue
			}
			if err := state.WriteMeta(srv.cfg.StateDir, sess); err != nil {
				slog.Warn("server: write meta after rename", "session", p.SessionID, "err", err)
			}
			slog.Info("server: rename", "session", p.SessionID, "slug", p.Slug, "title_len", len(p.Title), "req_id", env.ID)

		case protocol.IntentSetSessionName:
			var p protocol.SetSessionName
			if err := json.Unmarshal(env.Data, &p); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := requireSessionID(p.SessionID); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			name, err := protocol.NormalizeSessionName(p.Name)
			if err != nil {
				slog.Warn("server: set_name rejected", "session", p.SessionID, "reason", err, "req_id", env.ID)
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			sess, ok := srv.cfg.Store.Get(p.SessionID)
			if !ok {
				slog.Warn("server: set_name rejected", "session", p.SessionID, "reason", "unknown session", "req_id", env.ID)
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, "session not found")
				continue
			}
			changed, err := srv.cfg.Store.SetName(p.SessionID, name)
			if err != nil {
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, err.Error())
				continue
			}
			// Broadcast even when unchanged: it is the confirmation clients wait on.
			srv.commit(sess, map[string]any{"name": name})
			slog.Info("server: set_name", "session", p.SessionID, "cleared", name == "",
				"runes", len([]rune(name)), "changed", changed, "req_id", env.ID)

		case protocol.IntentRetire:
			var p protocol.Retire
			if err := json.Unmarshal(env.Data, &p); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := requireSessionID(p.SessionID); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			at, ttl, err := srv.retireDeadline(p.TTLSeconds)
			if err != nil {
				slog.Warn("retire: invalid ttl", "session", p.SessionID, "ttl_s", p.TTLSeconds, "err", err)
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			sess, ok := srv.cfg.Store.Get(p.SessionID)
			if !ok {
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, "session not found")
				continue
			}
			markedAt := srv.now().Round(0).UTC()
			changed, err := srv.cfg.Store.SetRetire(p.SessionID, at, markedAt)
			if err != nil {
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, err.Error())
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
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := requireSessionID(p.SessionID); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			sess, ok := srv.cfg.Store.Get(p.SessionID)
			if !ok {
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, "session not found")
				continue
			}
			remaining := time.Until(sess.Summary().RetireAt)
			changed, err := srv.cfg.Store.ClearRetire(p.SessionID)
			if errors.Is(err, state.ErrReaping) {
				slog.Warn("retire: unmark rejected; clearing", "session", p.SessionID)
				writeError(w, env.ID, protocol.ErrCodeBadState, err.Error())
				continue
			}
			if err != nil {
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, err.Error())
				continue
			}
			if changed {
				srv.commit(sess, map[string]any{"retire_at": nil, "retired_at": nil})
				slog.Info("retire: unmarked", "session", p.SessionID, "cause", "user", "remaining_s", int64(remaining.Seconds()))
			}

		case protocol.IntentSetSessionFleet:
			var p protocol.SetSessionFleet
			if err := json.Unmarshal(env.Data, &p); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			if err := requireSessionID(p.SessionID); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			sess, ok := srv.cfg.Store.Get(p.SessionID)
			if !ok {
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, "session not found")
				continue
			}
			if err := srv.cfg.Store.SetFleet(p.SessionID, p.Fleet); err != nil {
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, err.Error())
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
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
				continue
			}
			srv.SetMaxConcurrentSessions(p.N)

		// Shutdown and Reload are the socket forms of SIGTERM and SIGHUP, for
		// platforms (Windows) that cannot signal the daemon.
		case protocol.IntentShutdown:
			if cfg.OnShutdown == nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, "intent not implemented")
				continue
			}
			slog.Info("server: shutdown requested by client")
			cfg.OnShutdown()

		case protocol.IntentReload:
			if cfg.OnReload == nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, "intent not implemented")
				continue
			}
			if err := cfg.OnReload(); err != nil {
				writeError(w, env.ID, protocol.ErrCodeBadIntent, err.Error())
			}

		default:
			writeError(w, env.ID, protocol.ErrCodeBadIntent, "intent not implemented")
		}
	}
}
