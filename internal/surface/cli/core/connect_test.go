package core

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// serve listens on a short temp socket and runs handle for each connection.
func serve(t *testing.T, handle func(net.Conn)) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rexcore")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handle(conn)
		}
	}()
	return sock
}

func TestConnect(t *testing.T) {
	var ee ExitError

	_, _, err := Connect(filepath.Join(t.TempDir(), "none.sock"), "x")
	require.ErrorAs(t, err, &ee)
	require.Equal(t, ExitDaemonUnreachable, ee.Code)

	// A daemon that hangs up before answering Hello is unreachable too
	// (ResolveSelector used to return that raw, exit 1).
	hangup := serve(t, func(c net.Conn) { _ = c.Close() })
	_, _, err = Connect(hangup, "x")
	require.ErrorAs(t, err, &ee)
	require.Equal(t, ExitDaemonUnreachable, ee.Code)

	daemon := serve(t, func(c net.Conn) {
		defer c.Close()
		r, w := protocol.NewReader(c), protocol.NewWriter(c)
		env, err := r.Read()
		if err != nil {
			return
		}
		_ = w.WriteEvent(protocol.EventSnapshot, env.ID, protocol.Snapshot{Sessions: []protocol.SessionSummary{
			{ID: "7d4f3c8a-1234-4abc-89ab-cdefabcdef01", ShortID: "7d4f", Slug: "alpha"},
		}})
		_, _ = r.Read() // hold the connection until the client closes
	})
	c, sess, err := Connect(daemon, "alpha")
	require.NoError(t, err)
	require.Equal(t, "7d4f", sess.ShortID)
	require.NoError(t, c.Close())

	_, _, err = Connect(daemon, "nope")
	require.ErrorAs(t, err, &ee)
	require.Equal(t, ExitSelectorNotFound, ee.Code)
}
