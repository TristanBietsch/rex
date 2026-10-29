package server

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Rename and SetSessionFleet follow the "write meta, then broadcast" rule:
// whoever reacts to the SessionUpdated finds the change already on disk.
func TestMetaWrittenBeforeBroadcast(t *testing.T) {
	e := startRetireServer(t, doneSession("s1"))

	var mu sync.Mutex
	onDisk := map[string]string{} // patch key -> value read from meta.json at broadcast time
	cancel := e.store.Subscribe(func(ev state.Event) {
		if ev.Kind != state.EventUpdated {
			return
		}
		sess, err := state.LoadMeta(e.dir, ev.SessionID)
		if err != nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if _, ok := ev.Patch["fleet"]; ok {
			onDisk["fleet"] = sess.Fleet
		}
		if _, ok := ev.Patch["slug"]; ok {
			onDisk["slug"] = sess.Slug
		}
	})
	defer cancel()

	conn, w, r := e.dial(t)
	require.NoError(t, w.WriteIntent(protocol.IntentSetSessionFleet, "", protocol.SetSessionFleet{SessionID: "s1", Fleet: "backend"}))
	require.NoError(t, w.WriteIntent(protocol.IntentRename, "", protocol.Rename{SessionID: "s1", Slug: "renamed"}))
	waitEvent(t, conn, r, func(env protocol.Envelope) bool {
		mu.Lock()
		defer mu.Unlock()
		return onDisk["slug"] != ""
	})
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "backend", onDisk["fleet"])
	require.Equal(t, "renamed", onDisk["slug"])
}
