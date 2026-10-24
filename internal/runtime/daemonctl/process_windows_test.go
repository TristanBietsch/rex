//go:build windows

package daemonctl

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseTasklistPID(t *testing.T) {
	pid, err := parseTasklistPID("\"rex-daemon.exe\",\"4242\",\"Console\",\"1\",\"12,345 K\"\r\n")
	require.NoError(t, err)
	require.Equal(t, "4242", pid)

	_, err = parseTasklistPID("INFO: No tasks are running which match the specified criteria.\r\n")
	require.ErrorIs(t, err, ErrNotRunning)
}
