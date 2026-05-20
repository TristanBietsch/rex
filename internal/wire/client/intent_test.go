package client

import (
	"encoding/json"
	"net"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// readIntent blocks until one intent envelope arrives on serverConn.
func readIntent(t *testing.T, serverConn net.Conn) (string, []byte) {
	t.Helper()
	r := protocol.NewReader(serverConn)
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.KindIntent, env.Kind)
	return env.Type, env.Data
}

func TestClient_SubscribeWritesIntent(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	c := &Client{conn: clientConn, r: protocol.NewReader(clientConn), w: protocol.NewWriter(clientConn)}
	go func() { require.NoError(t, c.Subscribe("sess-1")) }()

	typ, data := readIntent(t, serverConn)
	require.Equal(t, protocol.IntentSubscribe, typ)
	var p protocol.Subscribe
	require.NoError(t, json.Unmarshal(data, &p))
	require.Equal(t, "sess-1", p.SessionID)
	require.False(t, p.Replay)
}

func TestClient_SubscribeReplayWritesIntent(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	c := &Client{conn: clientConn, r: protocol.NewReader(clientConn), w: protocol.NewWriter(clientConn)}
	go func() { require.NoError(t, c.SubscribeReplay("sess-2")) }()

	typ, data := readIntent(t, serverConn)
	require.Equal(t, protocol.IntentSubscribe, typ)
	var p protocol.Subscribe
	require.NoError(t, json.Unmarshal(data, &p))
	require.Equal(t, "sess-2", p.SessionID)
	require.True(t, p.Replay)
}

func TestClient_NewSessionWritesIntent(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	req := protocol.NewSession{ToolID: "echo", ModelID: "short", Slug: "t", CWD: "/tmp"}
	c := &Client{conn: clientConn, r: protocol.NewReader(clientConn), w: protocol.NewWriter(clientConn)}
	go func() { require.NoError(t, c.NewSession(req)) }()

	typ, data := readIntent(t, serverConn)
	require.Equal(t, protocol.IntentNewSession, typ)
	var p protocol.NewSession
	require.NoError(t, json.Unmarshal(data, &p))
	require.Equal(t, req.ToolID, p.ToolID)
	require.Equal(t, req.Slug, p.Slug)
}

func TestClient_SendInputAndReplyWriteIntents(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	c := &Client{conn: clientConn, r: protocol.NewReader(clientConn), w: protocol.NewWriter(clientConn)}

	go func() { require.NoError(t, c.SendInput("s1", []byte("hi"))) }()
	typ, data := readIntent(t, serverConn)
	require.Equal(t, protocol.IntentSendInput, typ)
	var in protocol.SendInput
	require.NoError(t, json.Unmarshal(data, &in))
	require.Equal(t, []byte("hi"), in.Bytes)

	go func() { require.NoError(t, c.Reply("s1", "yes")) }()
	typ, data = readIntent(t, serverConn)
	require.Equal(t, protocol.IntentReply, typ)
	var rep protocol.Reply
	require.NoError(t, json.Unmarshal(data, &rep))
	require.Equal(t, "yes", rep.Text)
}

func TestClient_RenameDeleteResizeFleetMaxConcurrent(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	c := &Client{conn: clientConn, r: protocol.NewReader(clientConn), w: protocol.NewWriter(clientConn)}

	check := func(wantType string, decode func([]byte)) {
		typ, data := readIntent(t, serverConn)
		require.Equal(t, wantType, typ)
		decode(data)
	}

	go func() { require.NoError(t, c.Rename("id", "slug", "title")) }()
	check(protocol.IntentRename, func(b []byte) {
		var p protocol.Rename
		require.NoError(t, json.Unmarshal(b, &p))
		require.Equal(t, "slug", p.Slug)
		require.Equal(t, "title", p.Title)
	})

	go func() { require.NoError(t, c.Delete("id")) }()
	check(protocol.IntentDelete, func(b []byte) {
		var p protocol.Delete
		require.NoError(t, json.Unmarshal(b, &p))
		require.Equal(t, "id", p.SessionID)
	})

	go func() { require.NoError(t, c.Resize("id", 80, 24)) }()
	check(protocol.IntentResize, func(b []byte) {
		var p protocol.Resize
		require.NoError(t, json.Unmarshal(b, &p))
		require.Equal(t, uint16(80), p.Cols)
	})

	go func() { require.NoError(t, c.SetSessionFleet("id", "fleet-a")) }()
	check(protocol.IntentSetSessionFleet, func(b []byte) {
		var p protocol.SetSessionFleet
		require.NoError(t, json.Unmarshal(b, &p))
		require.Equal(t, "fleet-a", p.Fleet)
	})

	go func() { require.NoError(t, c.SetMaxConcurrent(8)) }()
	check(protocol.IntentSetMaxConcurrent, func(b []byte) {
		var p protocol.SetMaxConcurrent
		require.NoError(t, json.Unmarshal(b, &p))
		require.Equal(t, 8, p.N)
	})

	go func() { require.NoError(t, c.FocusFilter("claude")) }()
	check(protocol.IntentFocusFilter, func(b []byte) {
		var p protocol.FocusFilter
		require.NoError(t, json.Unmarshal(b, &p))
		require.Equal(t, "claude", p.ToolID)
	})
}

func TestClient_DrainStopsWhenHandlerReturnsFalse(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { _ = clientConn.Close() })

	c := &Client{conn: clientConn, r: protocol.NewReader(clientConn), w: protocol.NewWriter(clientConn)}
	w := protocol.NewWriter(serverConn)

	done := make(chan error, 1)
	go func() {
		n := 0
		done <- c.Drain(func(protocol.Envelope) bool {
			n++
			return n < 1
		})
	}()

	require.NoError(t, w.WriteEvent(protocol.EventSnapshot, "", protocol.Snapshot{}))
	require.NoError(t, serverConn.Close())
	require.NoError(t, <-done)
}
