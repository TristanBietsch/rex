package setup

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// The default install (install.sh into ~/.local/bin) is a binary-swap
// install. rex update refuses it, and must do so before stopping the daemon
// instead of leaving it down.
func TestRunUpdate_BinarySwapRefusesBeforeStoppingDaemon(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// The test binary is not under GOPATH/bin, so this is a binary-swap install.
	t.Setenv("GOPATH", filepath.Join(home, "go"))
	// Short dir: unix socket paths cap at 104 bytes on macOS.
	rt, err := os.MkdirTemp("", "rexupd")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(rt) })
	t.Setenv("XDG_RUNTIME_DIR", rt)
	ln, err := net.Listen("unix", filepath.Join(rt, "rex.sock"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	stopped := false
	prev := stopDaemon
	stopDaemon = func() error { stopped = true; return nil }
	t.Cleanup(func() { stopDaemon = prev })

	err = RunUpdate([]string{"-yes"})
	var ee core.ExitError
	require.ErrorAs(t, err, &ee)
	require.Equal(t, core.ExitOperationRefused, ee.Code)
	require.False(t, stopped, "daemon must stay up when the upgrade is refused")
}
