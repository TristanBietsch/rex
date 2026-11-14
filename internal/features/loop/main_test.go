package loop

import (
	"testing"

	"github.com/tristanbietsch/rex/internal/testutil/testenv"
)

// TestMain runs the package's tests against a throwaway HOME and fails the
// package when a goroutine started by a test outlives it.
func TestMain(m *testing.M) {
	testenv.RunLeakChecked(m)
}
