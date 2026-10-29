package server

import (
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// transcriptReplayMax caps how many trailing bytes of the transcript we replay
// to a freshly-subscribed client. Big enough to cover an agent's recent screen
// (~64×400) without flooding the wire.
const transcriptReplayMax = 256 * 1024

// sendStoreEvent forwards a store event to the client in its wire form,
// gated on Hello like every board event.
func (c *client) sendStoreEvent(e state.Event) {
	switch e.Kind {
	case state.EventAdded:
		if e.Summary != nil {
			c.sendBoard(protocol.EventSessionAdded, *e.Summary, e.SessionID)
		}
	case state.EventUpdated:
		c.sendBoard(protocol.EventSessionUpdated, protocol.SessionUpdated{
			SessionID: e.SessionID, Patch: e.Patch,
		}, "")
	case state.EventRemoved:
		c.sendBoard(protocol.EventSessionRemoved, protocol.SessionRemoved{
			SessionID: e.SessionID, Reason: e.Reason,
		}, "")
	}
}
