package server

import (
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// A line the daemon can't use closes the connection, but only after an
// Error saying why — otherwise an old or broken client just sees EOF.
func TestClient_UnreadableLineGetsErrorThenClose(t *testing.T) {
	sock, _, cancel := startServer(t)
	defer cancel()

	cases := []struct {
		name string
		line string
		want string
	}{
		{"bad json", "{not json\n", "decode envelope"},
		{"wrong version", `{"v":2,"kind":"Intent","type":"Hello","data":{}}` + "\n", "unsupported protocol version"},
		{"oversize", `{"v":1,"kind":"Intent","type":"SendInput","data":{"bytes":"` +
			strings.Repeat("A", protocol.MaxIntentLineBytes) + `"}}` + "\n", "line too long"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conn, err := net.Dial("unix", sock)
			require.NoError(t, err)
			defer conn.Close()
			// The daemon stops reading an oversize line midway; don't block on it.
			go func() { _, _ = io.WriteString(conn, tc.line) }()

			r := protocol.NewReader(conn)
			require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
			env, err := r.Read()
			require.NoError(t, err)
			require.Equal(t, protocol.EventError, env.Type)
			ee := decodeError(t, env)
			require.Equal(t, protocol.ErrCodeBadIntent, ee.Code)
			require.Contains(t, ee.Message, tc.want)

			_, err = r.Read()
			require.ErrorIs(t, err, io.EOF, "connection should close after the error")
		})
	}
}
