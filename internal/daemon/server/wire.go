package server

import (
	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// transcriptReplayMax caps how many trailing bytes of the transcript we replay
// to a freshly-subscribed client. Big enough to cover an agent's recent screen
// (~64×400) without flooding the wire.
const transcriptReplayMax = 256 * 1024

// emitStoreEvent translates a store event into its wire event for c.
func (c *client) emitStoreEvent(e state.Event) {
	switch e.Kind {
	case state.EventAdded:
		if e.Summary != nil {
			c.sendEvent(protocol.EventSessionAdded, "", *e.Summary)
		}
	case state.EventUpdated:
		c.sendEvent(protocol.EventSessionUpdated, "", protocol.SessionUpdated{
			SessionID: e.SessionID, Patch: e.Patch,
		})
	case state.EventRemoved:
		c.sendEvent(protocol.EventSessionRemoved, "", protocol.SessionRemoved{
			SessionID: e.SessionID, Reason: e.Reason,
		})
	case state.EventReopened:
		// Clients get the caller's state patch instead.
	}
}
