package session

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// outputDaemon serves one connection: it answers Hello with a snapshot,
// then on Subscribe sends each chunk as a SessionOutput event and closes.
func outputDaemon(t *testing.T, sess protocol.SessionSummary, chunks ...string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rexaf") // short path: UDS names cap at ~104 bytes
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r, w := protocol.NewReader(conn), protocol.NewWriter(conn)
		for {
			env, err := r.Read()
			if err != nil {
				return
			}
			switch env.Type {
			case protocol.IntentHello:
				_ = w.WriteEvent(protocol.EventSnapshot, env.ID, protocol.Snapshot{Sessions: []protocol.SessionSummary{sess}})
			case protocol.IntentSubscribe:
				for _, ch := range chunks {
					_ = w.WriteEvent(protocol.EventSessionOutput, "", protocol.SessionOutput{SessionID: sess.ID, Bytes: []byte(ch)})
				}
				return // closes the connection: the stream ends
			}
		}
	}()
	return sock
}

// TestStreamAttach_FiltersReplayAndLive: agent output, replayed or live,
// keeps colors but loses OSC strings, even when one is split across events.
func TestStreamAttach_FiltersReplayAndLive(t *testing.T) {
	sess := protocol.SessionSummary{ID: "7d4f3c8a-1234-4abc-89ab-cdefabcdef01", ShortID: "7d4f", Slug: "alpha", ToolID: "ollama"}
	sock := outputDaemon(t, sess,
		"replay \x1b]52;c;aGk=\x07\x1b[31mok",
		"live \x1b]52;c;a", "Gk=\x07\x1b[32mgo")
	c, err := client.Dial(sock)
	require.NoError(t, err)
	defer c.Close()
	_, err = c.Hello("test")
	require.NoError(t, err)
	_, err = subscribeAttach(c, sess, true)
	require.NoError(t, err)

	var out strings.Builder
	require.NoError(t, streamAttach(c, sess.ID, nil, &out))
	require.Equal(t, "replay \x1b[31mok"+"live \x1b[32mgo", out.String())
	require.NotContains(t, out.String(), "]52;")
}

// TestExitAlt_ResetsKeyboardAndCursor: detach leaves VT52 mode first (CSI
// resets are ignored there), then resets cursor shape, synchronized output
// and the keyboard protocols an agent may have enabled.
func TestExitAlt_ResetsKeyboardAndCursor(t *testing.T) {
	for _, want := range []string{"\x1b[0 q", "\x1b[?2026l", "\x1b[>4;0m", "\x1b[<u"} {
		require.Contains(t, exitAlt, want)
	}
	require.True(t, strings.HasPrefix(exitAlt, "\x1b<"), "ESC < leaves VT52 mode and must come first")
	require.Less(t, strings.Index(exitAlt, "\x1b<"), strings.Index(exitAlt, "\x1b["))
}
