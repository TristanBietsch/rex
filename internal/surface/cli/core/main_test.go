package core

import (
	"testing"

	"github.com/tristanbietsch/rex/internal/testutil/testenv"
)

// TestMain runs the package's tests against a throwaway HOME so they can never
// read or write the developer's real rex config, state or daemon.
func TestMain(m *testing.M) {
	testenv.Run(m)
}
