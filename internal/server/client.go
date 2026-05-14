package server

import (
	"context"
	"net"

	"github.com/tristanbietsch/rex/internal/protocol"
)

func handleClient(ctx context.Context, conn net.Conn, cfg Config) {
	defer conn.Close()
	r := protocol.NewReader(conn)
	w := protocol.NewWriter(conn)

	// Read intents until ctx canceled or connection closes.
	for {
		if ctx.Err() != nil {
			return
		}
		env, err := r.Read()
		if err != nil {
			return
		}
		if env.Kind != protocol.KindIntent {
			_ = w.WriteEvent(protocol.EventError, env.ID, protocol.ErrorEvent{
				ID: env.ID, Code: protocol.ErrCodeBadIntent, Message: "expected an Intent",
			})
			continue
		}
		switch env.Type {
		case protocol.IntentHello:
			snap := protocol.Snapshot{Sessions: cfg.Store.Snapshot(), Filter: "all"}
			_ = w.WriteEvent(protocol.EventSnapshot, env.ID, snap)
		default:
			// Plan A only routes Hello; richer intents arrive in subsequent tasks.
			_ = w.WriteEvent(protocol.EventError, env.ID, protocol.ErrorEvent{
				ID: env.ID, Code: protocol.ErrCodeBadIntent, Message: "intent not implemented in Plan A",
			})
		}
	}
}
