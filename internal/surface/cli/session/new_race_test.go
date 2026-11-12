//go:build !windows

package session

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// racingSpawnDaemon answers Hello with an empty snapshot and a NewSession
// with another client's SessionAdded first (SessionAdded goes to every
// client), then the requested one.
func racingSpawnDaemon(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rexnew") // short path: UDS names cap at ~104 bytes
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
				_ = w.WriteEvent(protocol.EventSnapshot, env.ID, protocol.Snapshot{DaemonVersion: "test"})
			case protocol.IntentNewSession:
				other := protocol.SessionSummary{ID: "0ther000-0000-4000-8000-000000000000", ShortID: "0the", Slug: "theirs", ToolID: "echo"}
				mine := protocol.SessionSummary{ID: "m1ne0000-0000-4000-8000-000000000000", ShortID: "m1ne", Slug: "mine", ToolID: "echo"}
				_ = w.WriteEvent(protocol.EventSessionAdded, "", other)
				_ = w.WriteEvent(protocol.EventSessionAdded, "", mine)
			}
		}
	}()
	return sock
}

// rex new reports the session it spawned, not whichever SessionAdded
// arrives first.
func TestNew_ReportsItsOwnSession(t *testing.T) {
	sock := racingSpawnDaemon(t)
	out, _, err := actionRun(t, "json", func() error {
		return RunNew([]string{"-socket", sock, "-slug", "mine", "-wait", "5s", "do the thing"})
	})
	require.NoError(t, err)
	require.Contains(t, out, `"mine"`)
	require.NotContains(t, out, "theirs")
}
