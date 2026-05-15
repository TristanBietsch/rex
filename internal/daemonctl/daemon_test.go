package daemonctl

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReachable_NoSocket(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "missing.sock")
	require.False(t, Reachable(tmp))
}

func TestReachable_LiveSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "live.sock")
	ln, err := net.Listen("unix", sock)
	require.NoError(t, err)
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	require.True(t, Reachable(sock))
}

func TestReachable_StaleSocketFile(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "stale.sock")
	require.NoError(t, os.WriteFile(sock, []byte{}, 0o644))
	require.False(t, Reachable(sock))
}
