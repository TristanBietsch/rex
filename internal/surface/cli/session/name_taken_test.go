//go:build !windows

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

// nameTakenDaemon answers Hello with renameFixture and refuses every
// NewSession and SetSessionName with name_taken, echoing the request id.
func nameTakenDaemon(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rexnt") // short path: UDS names cap at ~104 bytes
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
				_ = w.WriteEvent(protocol.EventSnapshot, env.ID, protocol.Snapshot{Sessions: renameFixture})
			case protocol.IntentNewSession, protocol.IntentSetSessionName:
				_ = w.WriteEvent(protocol.EventError, env.ID, protocol.ErrorEvent{
					ID: env.ID, Code: protocol.ErrCodeNameTaken, Message: `name "Fix Auth" already in use by 7d4f`,
				})
			}
		}
	}()
	return sock
}

func TestRunName_NameTakenIsInvalidArgs(t *testing.T) {
	sock := nameTakenDaemon(t)
	err := RunName([]string{"-socket", sock, "beta", "fix", "auth"})
	var ee core.ExitError
	require.ErrorAs(t, err, &ee)
	require.Equal(t, core.ExitInvalidArgs, ee.Code)
	require.Equal(t, `name "Fix Auth" already in use by 7d4f`, ee.Msg)
}

func TestRunNew_NameTakenIsInvalidArgs(t *testing.T) {
	sock := nameTakenDaemon(t)
	err := RunNew([]string{"-socket", sock, "-slug", "fresh", "-name", "fix auth", "-wait", "5s", "task"})
	var ee core.ExitError
	require.ErrorAs(t, err, &ee)
	require.Equal(t, core.ExitInvalidArgs, ee.Code)
	require.Equal(t, `spawn failed: name "Fix Auth" already in use by 7d4f`, ee.Msg)
}

func TestExitForCode(t *testing.T) {
	require.Equal(t, core.ExitInvalidArgs, exitForCode(protocol.ErrCodeBadIntent))
	require.Equal(t, core.ExitInvalidArgs, exitForCode(protocol.ErrCodeNameTaken))
	require.Equal(t, core.ExitGeneric, exitForCode(protocol.ErrCodeSpawn))
	require.Equal(t, core.ExitGeneric, exitForCode("something_new"))
}
