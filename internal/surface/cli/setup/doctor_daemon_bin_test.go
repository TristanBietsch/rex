package setup

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// doctor finds rex-daemon where `rex daemon start` runs it from: next to
// rex first, then PATH. With rex run by full path and no PATH entry it used
// to report a false FAIL.
func TestCheckDaemonBinary_FindsDaemonNextToRex(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "rex-daemon")
	require.NoError(t, os.WriteFile(bin, []byte("x"), 0o600))
	t.Setenv("PATH", t.TempDir()) // nothing on PATH
	prev := findDaemonBinary
	findDaemonBinary = func() string { return bin }
	t.Cleanup(func() { findDaemonBinary = prev })

	got := checkDaemonBinary()
	require.Equal(t, checkPass, got.Status, "%+v", got)
	require.Equal(t, bin, got.Detail)

	findDaemonBinary = func() string { return "rex-daemon" } // not found anywhere
	require.Equal(t, checkFail, checkDaemonBinary().Status)
}
