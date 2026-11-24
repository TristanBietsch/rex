package server

import (
	"os"
	"testing"

	"github.com/tristanbietsch/rex/internal/testutil/testenv"
)

// TestMain runs the package's tests against a throwaway HOME (never the
// developer's real rex config, state or daemon) and fails the package when a
// goroutine started by a test outlives it.
//
// When started with inSessionHelperEnv set, the test binary is instead a
// child process that talks to a test daemon (see insession_test.go): a
// descendant of the daemon's process, as an agent's tool call would be.
func TestMain(m *testing.M) {
	if sock := os.Getenv(inSessionHelperEnv); sock != "" {
		os.Exit(runInSessionHelper(sock, os.Getenv(inSessionIntentEnv)))
	}
	testenv.RunLeakChecked(m)
}
