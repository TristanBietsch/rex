package lua

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package when a goroutine started by a test outlives it.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
