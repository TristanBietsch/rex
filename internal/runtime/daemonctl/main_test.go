package daemonctl

import (
	"os"
	"testing"

	"github.com/tristanbietsch/rex/internal/testutil/testenv"
)

// TestMain runs the package's tests against a throwaway HOME (never the
// developer's real rex config, state or daemon) and fails the package when a
// goroutine started by a test outlives it.
//
// When Start spawns the test binary as a fake rex-daemon, fakeModeEnv is set
// and the binary acts as that daemon instead of running tests.
func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeModeEnv); mode != "" {
		os.Exit(runFakeDaemonProcess(mode, os.Args[1:]))
	}
	testenv.RunLeakChecked(m)
}
