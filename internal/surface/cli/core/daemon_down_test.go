package core

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/client"
)

func shortSock(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "rexcore")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "d.sock")
}

func asExit(t *testing.T, err error) ExitError {
	t.Helper()
	var ee ExitError
	require.True(t, errors.As(err, &ee), "%v", err)
	return ee
}

func TestDial_NoDaemonMessage(t *testing.T) {
	sock := shortSock(t)
	_, err := DialPath(sock)
	ee := asExit(t, err)
	require.Equal(t, ExitDaemonUnreachable, ee.Code)
	require.Equal(t, "rex-daemon is not running (no socket at "+sock+"); start it with `rex daemon start`", ee.Msg)
	require.Equal(t, "rex daemon start", ee.Hint)
	require.Error(t, ee.Err, "the dial error stays as the cause")
}

// A socket file left behind by a dead daemon refuses the connection.
func TestDial_StaleSocketMessage(t *testing.T) {
	sock := shortSock(t)
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	ln.(*net.UnixListener).SetUnlinkOnClose(false)
	require.NoError(t, ln.Close())
	require.FileExists(t, sock)

	_, err = DialPath(sock)
	ee := asExit(t, err)
	require.Equal(t, ExitDaemonUnreachable, ee.Code)
	require.Contains(t, ee.Msg, "rex-daemon is not running")
	require.Contains(t, ee.Msg, sock)
}

// Another dial failure is still exit 4, with its own message.
func TestDial_OtherErrorKeepsMessage(t *testing.T) {
	f := filepath.Join(t.TempDir(), "plain")
	require.NoError(t, os.WriteFile(f, nil, 0o600))
	_, err := DialPath(filepath.Join(f, "sub.sock")) // ENOTDIR
	ee := asExit(t, err)
	require.Equal(t, ExitDaemonUnreachable, ee.Code)
	require.NotContains(t, ee.Msg, "is not running")
}

// A daemon that accepts and hangs up fails the handshake: exit 4, not 1.
func TestResolveSelector_HelloFailExit4(t *testing.T) {
	sock := shortSock(t)
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	c, err := client.Dial(sock)
	require.NoError(t, err)
	defer c.Close()
	_, err = ResolveSelector(c, "x")
	ee := asExit(t, err)
	require.Equal(t, ExitDaemonUnreachable, ee.Code)
	require.Contains(t, ee.Msg, "daemon")
}
