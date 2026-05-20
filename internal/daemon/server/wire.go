package server

import (
	"log/slog"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// transcriptReplayMax caps how many trailing bytes of the transcript we replay
// to a freshly-subscribed client. Big enough to cover an agent's recent screen
// (~64×400) without flooding the wire.
const transcriptReplayMax = 256 * 1024

func emitEvent(w *protocol.Writer, e state.Event) {
	switch e.Kind {
	case state.EventAdded:
		if e.Summary != nil {
			writeEvent(w, protocol.EventSessionAdded, "", *e.Summary)
		}
	case state.EventUpdated:
		writeEvent(w, protocol.EventSessionUpdated, "", protocol.SessionUpdated{
			SessionID: e.SessionID, Patch: e.Patch,
		})
	case state.EventRemoved:
		writeEvent(w, protocol.EventSessionRemoved, "", protocol.SessionRemoved{
			SessionID: e.SessionID,
		})
	}
}

func writeError(w *protocol.Writer, id, code, msg string) {
	writeEvent(w, protocol.EventError, id, protocol.ErrorEvent{ID: id, Code: code, Message: msg})
}

// writeEvent logs at debug when the client connection is gone (common on disconnect).
func writeEvent(w *protocol.Writer, eventType, id string, data any) {
	if err := w.WriteEvent(eventType, id, data); err != nil {
		slog.Debug("server: write event failed", "type", eventType, "id", id, "err", err)
	}
}
