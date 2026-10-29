package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// A client that stops reading (a suspended TUI) must not stall store
// updates — supervisor ticks, the PTY reader and every other client depend
// on them. It gets disconnected instead.
func TestSlowClient_DoesNotStallStore(t *testing.T) {
	store := state.NewStore()
	require.NoError(t, store.Add(&state.Session{ID: "sess-1", ShortID: "s1", Slug: "s", State: protocol.StateWorking}))
	sock, _, cancel := startServerWithStore(t, store)
	defer cancel()

	stuck, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer stuck.Close()
	w := protocol.NewWriter(stuck)
	require.NoError(t, w.WriteIntent(protocol.IntentHello, "h", protocol.Hello{}))
	// Never read from stuck. Let the Hello land before flooding.
	time.Sleep(100 * time.Millisecond)

	pad := strings.Repeat("x", 4000)
	const n = 3000
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for i := 0; i < n; i++ {
			_ = store.UpdateLastLine("sess-1", fmt.Sprintf("%s %d", pad, i))
		}
	}()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("store updates blocked behind a client that stopped reading")
	}

	// Other clients are still served, and see the latest state.
	fresh, err := net.Dial("unix", sock)
	require.NoError(t, err)
	defer fresh.Close()
	fw := protocol.NewWriter(fresh)
	fr := protocol.NewReader(fresh)
	require.NoError(t, fw.WriteIntent(protocol.IntentHello, "h", protocol.Hello{}))
	require.NoError(t, fresh.SetReadDeadline(time.Now().Add(2*time.Second)))
	env, err := fr.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.EventSnapshot, env.Type)
	var snap protocol.Snapshot
	require.NoError(t, json.Unmarshal(env.Data, &snap))
	require.Len(t, snap.Sessions, 1)
	require.True(t, strings.HasSuffix(snap.Sessions[0].LastLine, fmt.Sprintf(" %d", n-1)))

	// The stuck client was dropped: once its socket buffer drains, EOF.
	require.NoError(t, stuck.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, err = io.Copy(io.Discard, stuck)
	require.NoError(t, err, "expected EOF (client dropped), got a read error")
}
