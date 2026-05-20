package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

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
				if strings.Contains(err.Error(), "too many concurrent sessions") {
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
			// Tear down a running supervisor first so its goroutines don't race with
			// the on-disk cleanup below.
			srv.StopSession(p.SessionID)
			if err := cfg.Store.Remove(p.SessionID); err != nil {
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, err.Error())
				continue
			}
			// Disk cleanup is best-effort — the session is already gone from memory.
			_ = state.RemoveSessionDir(cfg.StateDir, p.SessionID)
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
				if p.Replay {
					if tail, err := state.TranscriptTail(srv.TranscriptDir(), p.SessionID, transcriptReplayMax); err == nil && len(tail) > 0 {
						writeEvent(w, protocol.EventSessionOutput, "", protocol.SessionOutput{
							SessionID: p.SessionID, Bytes: tail,
						})
					}
				}
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
			select {
			case ch <- []byte(p.Text + "\n"):
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
			sess, ok := srv.cfg.Store.Get(p.SessionID)
			if !ok {
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, "session not found")
				continue
			}
			if err := srv.cfg.Store.UpdateRename(p.SessionID, p.Slug, p.Title); err != nil {
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, err.Error())
				continue
			}
			if err := state.WriteMeta(srv.cfg.StateDir, sess); err != nil {
				slog.Warn("server: write meta after rename", "session", p.SessionID, "err", err)
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

		default:
			writeError(w, env.ID, protocol.ErrCodeBadIntent, "intent not implemented")
		}
	}
}
