package daemonctl

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckSocketPath(t *testing.T) {
	require.NoError(t, CheckSocketPath(strings.Repeat("a", sunPathLen-1)))
	err := CheckSocketPath(strings.Repeat("a", sunPathLen))
	require.Error(t, err)
	require.Contains(t, err.Error(), "allows at most")
}

// The limit must match what the OS actually binds: the longest accepted
// path binds, one byte more doesn't.
func TestCheckSocketPath_MatchesBind(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("AF_UNIX support varies by Windows build")
	}
	dir, err := os.MkdirTemp("", "rx")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	pad := sunPathLen - 1 - len(dir) - 1
	require.Positive(t, pad, "temp dir too long for this test: %s", dir)

	ok := filepath.Join(dir, strings.Repeat("s", pad))
	require.NoError(t, CheckSocketPath(ok))
	ln, err := net.Listen("unix", ok)
	require.NoError(t, err)
	require.NoError(t, ln.Close())

	long := ok + "s"
	require.Error(t, CheckSocketPath(long))
	_, err = net.Listen("unix", long)
	require.Error(t, err)
}
