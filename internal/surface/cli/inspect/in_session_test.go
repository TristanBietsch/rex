package inspect

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// refusingDaemon listens on a fresh socket and answers every intent with
// Error in_session, as rex-daemon answers a caller inside a rex session.
func refusingDaemon(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rex-in")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "rex.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			r, w := protocol.NewReader(conn), protocol.NewWriter(conn)
			for {
				env, err := r.Read()
				if err != nil {
					break
				}
				_ = w.WriteEvent(protocol.EventError, env.ID, protocol.ErrorEvent{
					ID: env.ID, Code: protocol.ErrCodeInSession, Message: "refused: inside a rex session"})
			}
			_ = conn.Close()
		}
	}()
	t.Cleanup(func() { _ = ln.Close(); <-done })
	return sock
}

// Inside a rex session the daemon refuses the handshake. digest and stats
// must say so (exit 6), not read every session from disk instead.
func TestDigestAndStats_RefusedInSessionDoNotFallBackToDisk(t *testing.T) {
	sock := refusingDaemon(t)
	for name, run := range map[string]func([]string) error{"digest": RunDigest, "stats": RunStats} {
		t.Run(name, func(t *testing.T) {
			_, err := runIn(t, "txt", func() error { return run([]string{"-socket", sock}) })
			var ec core.ExitCoder
			require.True(t, errors.As(err, &ec), "want an exit error, got %v", err)
			require.Equal(t, int(core.ExitOperationRefused), ec.ExitCode())
			require.ErrorContains(t, err, "inside a rex session")
		})
	}
}
