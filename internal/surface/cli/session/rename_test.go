package session

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

var renameFixture = []protocol.SessionSummary{
	{ID: "7d4f3c8a-1234-4abc-89ab-cdefabcdef01", ShortID: "7d4f", Slug: "alpha"},
	{ID: "b203a999-1234-4abc-89ab-cdefabcdef02", ShortID: "b203", Slug: "beta"},
}

// fakeDaemon serves one connection: Snapshot on Hello, then answers the
// Rename intent via reply (which gets the intent envelope). It returns the
// socket path and a channel carrying the Rename envelope it saw.
func fakeDaemon(t *testing.T, reply func(w *protocol.Writer, env protocol.Envelope)) (string, <-chan protocol.Envelope) {
	t.Helper()
	dir, err := os.MkdirTemp("", "rexcli")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	seen := make(chan protocol.Envelope, 1)
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
				_ = w.WriteEvent(protocol.EventSnapshot, env.ID, protocol.Snapshot{Sessions: renameFixture})
			case protocol.IntentRename:
				seen <- env
				reply(w, env)
			}
		}
	}()
	return sock, seen
}

func TestRunRename_ReportsDaemonRejection(t *testing.T) {
	sock, seen := fakeDaemon(t, func(w *protocol.Writer, env protocol.Envelope) {
		// An unrelated error first: must be ignored (different id).
		_ = w.WriteEvent(protocol.EventError, "other", protocol.ErrorEvent{ID: "other", Code: protocol.ErrCodeSpawn, Message: "nope"})
		_ = w.WriteEvent(protocol.EventError, env.ID, protocol.ErrorEvent{
			ID: env.ID, Code: protocol.ErrCodeBadIntent, Message: `slug "alpha" already in use by 7d4f`,
		})
	})

	err := RunRename([]string{"-socket", sock, "beta", "alpha"})
	var ee core.ExitError
	require.ErrorAs(t, err, &ee)
	require.Equal(t, core.ExitInvalidArgs, ee.Code)
	require.Equal(t, `slug "alpha" already in use by 7d4f`, ee.Msg)

	env := <-seen
	require.NotEmpty(t, env.ID, "rename must carry a request id")
}

func TestRunRename_WaitsForConfirmation(t *testing.T) {
	sock, _ := fakeDaemon(t, func(w *protocol.Writer, env protocol.Envelope) {
		_ = w.WriteEvent(protocol.EventSessionUpdated, "", protocol.SessionUpdated{
			SessionID: renameFixture[1].ID, Patch: map[string]any{"slug": "gamma"},
		})
	})
	require.NoError(t, RunRename([]string{"-socket", sock, "beta", "gamma"}))
}

func TestRunRename_RejectsBadSlugLocally(t *testing.T) {
	// No daemon needed: format errors are caught before dialing.
	err := RunRename([]string{"-socket", "/nonexistent.sock", "beta", "has space"})
	var ee core.ExitError
	require.ErrorAs(t, err, &ee)
	require.Equal(t, core.ExitInvalidArgs, ee.Code)
	require.Contains(t, ee.Msg, "whitespace")
}
