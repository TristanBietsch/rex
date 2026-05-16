package daemonctl

import (
	"net"
	"os"
	"os/exec"
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

func TestFindBinary_NextToSelf(t *testing.T) {
	dir := t.TempDir()
	candidate := filepath.Join(dir, "rex-daemon")
	require.NoError(t, os.WriteFile(candidate, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	// Simulate "rex" living in dir by passing dir explicitly.
	got := findBinaryIn(dir)
	require.Equal(t, candidate, got)
}

func TestFindBinary_PathFallback(t *testing.T) {
	// If neither sibling nor PATH has it, returns the literal name.
	got := findBinaryIn(t.TempDir())
	if _, err := exec.LookPath("rex-daemon"); err != nil {
		require.Equal(t, "", got)
	}
}
