//go:build !windows

package daemonctl

import (
	"os"
	"os/exec"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// S-08: `pgrep rex-daemon` matched substrings across every user. The lookup
// must be exact-name and limited to the current UID.
func TestPgrepArgs_ExactNameCurrentUser(t *testing.T) {
	require.Equal(t, []string{"-x", "-u", strconv.Itoa(os.Getuid()), "rex-daemon"}, pgrepArgs())
}

func TestParsePgrep(t *testing.T) {
	pid, err := parsePgrep([]byte("4242\n"), nil)
	require.NoError(t, err)
	require.Equal(t, "4242", pid)

	// pgrep exits 1 when nothing matches.
	noMatch := exec.Command("sh", "-c", "exit 1").Run()
	_, err = parsePgrep(nil, noMatch)
	require.ErrorIs(t, err, ErrNotRunning)

	_, err = parsePgrep([]byte("\n"), nil)
	require.ErrorIs(t, err, ErrNotRunning)

	// Several daemons (e.g. on different sockets): the PID cannot say which
	// one a caller means, so there is no answer.
	_, err = parsePgrep([]byte("4242\n4343\n"), nil)
	require.ErrorIs(t, err, ErrMultipleDaemons)
	require.ErrorContains(t, err, "multiple")
	require.ErrorContains(t, err, "4242, 4343")
}
