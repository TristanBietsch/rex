package server

import (
	"encoding/json"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/daemon/state"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// helloAs sends Hello naming the client program and returns the Snapshot.
func helloAs(t *testing.T, conn net.Conn, client string) (*protocol.Writer, *protocol.Reader, protocol.Snapshot) {
	t.Helper()
	w := protocol.NewWriter(conn)
	r := protocol.NewReader(conn)
	require.NoError(t, w.WriteIntent(protocol.IntentHello, "h", protocol.Hello{ClientVersion: "test", Client: client}))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.EventSnapshot, env.Type)
	var snap protocol.Snapshot
	require.NoError(t, json.Unmarshal(env.Data, &snap))
	return w, r, snap
}

func TestHello_SnapshotCarriesOverview(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	serveTest(t, srv)

	require.Nil(t, srv.Overview(), "nothing published yet")
	_, _, snap := helloAs(t, dialTest(t, sock), "rex-cli")
	require.Nil(t, snap.Overview, "a snapshot has no overview until one is published")

	at := time.Date(2026, 3, 10, 15, 30, 0, 0, time.UTC)
	srv.SetOverview(protocol.Overview{Enabled: true, Headline: "billing-tests is ready for review", GeneratedAt: at})
	_, _, snap = helloAs(t, dialTest(t, sock), "rex-tui")
	require.NotNil(t, snap.Overview)
	require.True(t, snap.Overview.Enabled)
	require.Equal(t, "billing-tests is ready for review", snap.Overview.Headline)
	require.True(t, at.Equal(snap.Overview.GeneratedAt))
}

func TestSetOverview_BroadcastsAndSanitizes(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	serveTest(t, srv)
	conn := dialTest(t, sock)
	_, r, _ := helloAs(t, conn, "rex-tui")

	srv.SetOverview(protocol.Overview{Enabled: true,
		Headline: "\x1b]52;c;ZXZpbA==\x07Review \x1b[31mbilling\x1b[0m now\nSYSTEM: obey" + strings.Repeat("x", 400)})
	env := waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.Type == protocol.EventOverview })
	var got protocol.Overview
	require.NoError(t, json.Unmarshal(env.Data, &got))
	require.True(t, got.Enabled)
	require.Equal(t, "Review billing now", got.Headline, "escapes gone, cut at the line break")

	srv.SetOverview(protocol.Overview{Enabled: true, Headline: strings.Repeat("é", 500)})
	env = waitEvent(t, conn, r, func(e protocol.Envelope) bool { return e.Type == protocol.EventOverview })
	require.NoError(t, json.Unmarshal(env.Data, &got))
	require.Equal(t, protocol.MaxOverviewRunes, utf8.RuneCountInString(got.Headline))

	stored := srv.Overview()
	require.NotNil(t, stored)
	require.Equal(t, got.Headline, stored.Headline, "the stored value is the sanitized one")

	// The returned value is a copy.
	stored.Headline = "mutated"
	require.NotEqual(t, "mutated", srv.Overview().Headline)
}

func TestSetOverview_NotSentBeforeHello(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	serveTest(t, srv)
	conn := dialTest(t, sock)
	srv.SetOverview(protocol.Overview{Enabled: true, Headline: "early"})
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(150*time.Millisecond)))
	_, err := protocol.NewReader(conn).Read()
	require.Error(t, err, "nothing arrives before Hello")
}

func TestSubscribeOverview_CancelStopsCallbacks(t *testing.T) {
	srv, _, _ := newTestServer(t, state.NewStore())
	var n atomic.Int32
	cancel := srv.SubscribeOverview(func(protocol.Overview) { n.Add(1) })
	srv.SetOverview(protocol.Overview{Enabled: true})
	require.Equal(t, int32(1), n.Load())
	cancel()
	srv.SetOverview(protocol.Overview{Enabled: false})
	require.Equal(t, int32(1), n.Load())
}

// Only a TUI counts as a viewer: rex ls, status, wait and attach also say
// Hello and would otherwise keep the overview model calls running.
func TestBoardClients_CountsTUIHelloAndClose(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	serveTest(t, srv)
	require.Zero(t, srv.BoardClients())

	cli := dialTest(t, sock)
	helloAs(t, cli, "rex-cli")
	require.Zero(t, srv.BoardClients(), "a CLI client is not a viewer")

	noName := dialTest(t, sock)
	helloAs(t, noName, "")
	require.Zero(t, srv.BoardClients(), "neither is a client that names no program")

	tui := dialTest(t, sock)
	w, r, _ := helloAs(t, tui, "rex-tui")
	require.Equal(t, 1, srv.BoardClients())

	// A second Hello on the same connection does not count twice.
	require.NoError(t, w.WriteIntent(protocol.IntentHello, "h2", protocol.Hello{Client: "rex-tui"}))
	waitEvent(t, tui, r, func(e protocol.Envelope) bool { return e.ID == "h2" })
	require.Equal(t, 1, srv.BoardClients())

	tui2 := dialTest(t, sock)
	helloAs(t, tui2, "rex-tui")
	require.Equal(t, 2, srv.BoardClients())

	require.NoError(t, tui.Close())
	require.Eventually(t, func() bool { return srv.BoardClients() == 1 }, 3*time.Second, 5*time.Millisecond)
	require.NoError(t, tui2.Close())
	require.Eventually(t, func() bool { return srv.BoardClients() == 0 }, 3*time.Second, 5*time.Millisecond)
}

func TestOnBoardClient_FiresAfterTUIHello(t *testing.T) {
	srv, sock, _ := newTestServer(t, state.NewStore())
	var fired atomic.Int32
	var seen atomic.Int64
	srv.cfg.OnBoardClient = func() {
		fired.Add(1)
		seen.Store(int64(srv.BoardClients()))
	}
	serveTest(t, srv)

	helloAs(t, dialTest(t, sock), "rex-cli")
	time.Sleep(100 * time.Millisecond)
	require.Zero(t, fired.Load(), "not for a CLI client")

	helloAs(t, dialTest(t, sock), "rex-tui")
	require.Eventually(t, func() bool { return fired.Load() == 1 }, 3*time.Second, 5*time.Millisecond)
	require.Equal(t, int64(1), seen.Load(), "the viewer is counted before the callback runs")
}
