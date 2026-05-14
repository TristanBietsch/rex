package server

import (
	"context"
	"encoding/json"
	"fmt"
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
			if err := cfg.Store.Remove(p.SessionID); err != nil {
				writeError(w, env.ID, protocol.ErrCodeUnknownSession, err.Error())
			}
		default:
			writeError(w, env.ID, protocol.ErrCodeBadIntent, "intent not implemented in Plan A")
		}
	}
}

func handleNewSession(ctx context.Context, intentID string, p protocol.NewSession, srv *Server, w *protocol.Writer) error {
	cfg := srv.cfg
	tool, model, ok := cfg.Registry.FindModel(p.ToolID, p.ModelID)
	if !ok {
		return fmt.Errorf("tool %s/%s not in registry", p.ToolID, p.ModelID)
	}

	if !srv.TryAcquireSession() {
		return fmt.Errorf("too many concurrent sessions (cap=%d)", cfg.MaxConcurrentSessions)
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

	sup := pty.New(pty.SupervisorConfig{
		StateDir:   cfg.StateDir,
		Store:      cfg.Store,
		Command:    cmdArgs,
		CWD:        p.CWD,
		Adapter:    ad,
		OutputSink: nil, // Plan A: subscribers consume via SessionOutput in Plan B; for now drop.
	})

	// Run in a background goroutine; the store events drive the wire.
	go func() {
		defer srv.ReleaseSession()
		_ = sup.Run(ctx, sess)
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
