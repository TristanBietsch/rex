package server

import (
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// transcriptReplayMax caps how many trailing bytes of the transcript we replay
// to a freshly-subscribed client. Big enough to cover an agent's recent screen
// (~64×400) without flooding the wire.
const transcriptReplayMax = 256 * 1024

// sendStoreEvent queues a store event in its wire form.
func (c *client) sendStoreEvent(e state.Event) {
	switch e.Kind {
	case state.EventAdded:
		if e.Summary != nil {
			c.send(protocol.EventSessionAdded, "", *e.Summary)
		}
	case state.EventUpdated:
		c.send(protocol.EventSessionUpdated, "", protocol.SessionUpdated{
			SessionID: e.SessionID, Patch: e.Patch,
		})
	case state.EventRemoved:
		c.send(protocol.EventSessionRemoved, "", protocol.SessionRemoved{
			SessionID: e.SessionID, Reason: e.Reason,
		})
	}
}
