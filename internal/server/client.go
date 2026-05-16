package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tristanbietsch/rex/internal/adapter"
	"github.com/tristanbietsch/rex/internal/ids"
	"github.com/tristanbietsch/rex/internal/protocol"
	"github.com/tristanbietsch/rex/internal/pty"
	"github.com/tristanbietsch/rex/internal/state"
)

// transcriptReplayMax caps how many trailing bytes of the transcript we replay
// to a freshly-subscribed client. Big enough to cover an agent's recent screen
// (~64×400) without flooding the wire.
const transcriptReplayMax = 256 * 1024

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
		_ = w.WriteEvent(protocol.EventSummarizerHealth, "", h)
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
			_ = w.WriteEvent(protocol.EventSnapshot, env.ID, snap)
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
				if p.Replay {
					if tail, err := state.TranscriptTail(srv.TranscriptDir(), p.SessionID, transcriptReplayMax); err == nil && len(tail) > 0 {
						_ = w.WriteEvent(protocol.EventSessionOutput, "", protocol.SessionOutput{
							SessionID: p.SessionID, Bytes: tail,
						})
					}
				}
				// Register an output sink for this session for the rest of the connection.
				outCancel := srv.SubscribeSessionOutput(p.SessionID, func(b []byte) {
					_ = w.WriteEvent(protocol.EventSessionOutput, "", protocol.SessionOutput{
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
			sess, ok := srv.cfg.Store.Get(p.SessionID)
			if !ok {
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, "session not found")
				continue
			}
			patch := map[string]any{}
			if p.Slug != "" {
				sess.Slug = p.Slug
				patch["slug"] = p.Slug
			}
			if p.Title != "" {
				sess.Title = p.Title
				patch["title"] = p.Title
			}
			// Trigger a SessionUpdated event by touching last_line (a small reuse hack).
			_ = srv.cfg.Store.UpdateLastLine(p.SessionID, sess.LastLine)
			_ = patch

		case protocol.IntentSetSessionFleet:
		var p protocol.SetSessionFleet
		if err := json.Unmarshal(env.Data, &p); err != nil {
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

func handleNewSession(ctx context.Context, intentID string, p protocol.NewSession, srv *Server, w *protocol.Writer) error {
	cfg := srv.cfg
	// Read the registry through the lock so SIGHUP-swapped versions take effect.
	tool, model, ok := srv.Registry().FindModel(p.ToolID, p.ModelID)
	if !ok {
		return fmt.Errorf("tool %s/%s not in registry", p.ToolID, p.ModelID)
	}

	if !srv.TryAcquireSession() {
		return fmt.Errorf("too many concurrent sessions (cap=%d)", srv.MaxConcurrentSessions())
	}
	// From here, must release on error.

	id := ids.NewSessionID()
	// Disambiguate against the live set.
	taken := make(map[string]struct{})
	for _, s := range cfg.Store.All() {
		taken[s.ShortID] = struct{}{}
	}
	short := ids.ExtendShortID(id, taken)

	cmdArgs := append([]string{}, tool.Command...)
	cmdArgs = append(cmdArgs, model.Args...)
	// Apply the chosen reasoning effort by interpolating model.effort.arg_template.
	// We split the rendered template on whitespace so multi-token flags like
	// `-c key=value` work (each becomes its own argv slot).
	if model.Effort != nil && p.Effort != "" && model.Effort.ArgTemplate != "" {
		rendered := strings.ReplaceAll(model.Effort.ArgTemplate, "{value}", p.Effort)
		cmdArgs = append(cmdArgs, strings.Fields(rendered)...)
	}

	sess := &state.Session{
		ID:        id,
		ShortID:   short,
		ToolID:    p.ToolID,
		ModelID:   p.ModelID,
		Effort:    p.Effort,
		Slug:      p.Slug,
		Title:     p.Title,
		CWD:       p.CWD,
		State:     protocol.StateQueued,
		StartedAt: time.Now().UTC(),
		Fleet:     p.Fleet,
	}
	if err := cfg.Store.Add(sess); err != nil {
		srv.ReleaseSession()
		return err
	}

	ad, err := adapter.For(tool)
	if err != nil {
		srv.ReleaseSession()
		_ = cfg.Store.Remove(sess.ID)
		return fmt.Errorf("build adapter: %w", err)
	}

	inputCh := make(chan []byte, 16)
	srv.RegisterInputChannel(sess.ID, inputCh)

	completeCh := make(chan struct{}, 1)

	sup := pty.New(pty.SupervisorConfig{
		StateDir:       cfg.StateDir,
		Store:          cfg.Store,
		Command:        cmdArgs,
		CWD:            p.CWD,
		Adapter:        ad,
		InputCh:        inputCh,
		CompleteCh:     completeCh,
		InitialPrompt:  p.InitialPrompt,
		SummaryRequest: cfg.SummaryRequest,
		OutputSink: func(b []byte) {
			srv.broadcastSessionOutput(sess.ID, b)
		},
		RegisterResize: func(fn func(cols, rows uint16) error) {
			srv.RegisterResize(sess.ID, fn)
		},
		UnregisterResize: func() {
			srv.UnregisterResize(sess.ID)
		},
	})

	// Per-session ctx + done so IntentDelete can synchronously tear the PTY down.
	sessCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	srv.RegisterStop(sess.ID, func() {
		cancel()
		<-done
	})
	srv.RegisterComplete(sess.ID, func() {
		select {
		case completeCh <- struct{}{}:
		default:
			// already pending; further signals are no-ops
		}
	})

	// Run in a background goroutine; the store events drive the wire.
	go func() {
		defer close(done)
		defer srv.UnregisterStop(sess.ID)
		defer srv.UnregisterComplete(sess.ID)
		defer srv.UnregisterInputChannel(sess.ID)
		defer srv.ReleaseSession()
		_ = sup.Run(sessCtx, sess)
	}()
	_ = intentID
	_ = w
	return nil
}

func emitEvent(w *protocol.Writer, e state.Event) {
	switch e.Kind {
	case state.EventAdded:
		if e.Summary != nil {
			_ = w.WriteEvent(protocol.EventSessionAdded, "", *e.Summary)
		}
	case state.EventUpdated:
		_ = w.WriteEvent(protocol.EventSessionUpdated, "", protocol.SessionUpdated{
			SessionID: e.SessionID, Patch: e.Patch,
		})
	case state.EventRemoved:
		_ = w.WriteEvent(protocol.EventSessionRemoved, "", protocol.SessionRemoved{
			SessionID: e.SessionID,
		})
	}
}

func writeError(w *protocol.Writer, id, code, msg string) {
	_ = w.WriteEvent(protocol.EventError, id, protocol.ErrorEvent{ID: id, Code: code, Message: msg})
}
