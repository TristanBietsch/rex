package client

import (
	"encoding/json"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/protocol"
)

func TestClient_CompleteWritesIntent(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	c := &Client{
		conn: clientConn,
		r:    protocol.NewReader(clientConn),
		w:    protocol.NewWriter(clientConn),
	}

	done := make(chan error, 1)
	go func() { done <- c.Complete("sess-1") }()

	r := protocol.NewReader(serverConn)
	env, err := r.Read()
	require.NoError(t, err)
	require.Equal(t, protocol.KindIntent, env.Kind)
	require.Equal(t, protocol.IntentComplete, env.Type)

	var p protocol.Complete
	require.NoError(t, json.Unmarshal(env.Data, &p))
	require.Equal(t, "sess-1", p.SessionID)

	require.NoError(t, <-done)
}
