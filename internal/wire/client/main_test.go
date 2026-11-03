package client

import (
	"testing"

	"github.com/tristanbietsch/rex/internal/testutil/testenv"
)

// TestMain runs the package's tests against a throwaway HOME (never the
// developer's real rex config, state or daemon) and fails the package when a
// goroutine started by a test outlives it.
func TestMain(m *testing.M) {
	testenv.RunLeakChecked(m)
}
