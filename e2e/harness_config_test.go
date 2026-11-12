package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Scripts run with a PATH of their private build copies plus system dirs, so
// an installed agent (~/.local/bin, homebrew) is never found; and daemon-start
// refuses a config that leaves model auto-detect on (see daemon_test.go), so
// no e2e daemon runs a model-list command.
func TestHarnessPathHasNoAgentDirs(t *testing.T) {
	require.Equal(t, "/usr/bin:/bin:/usr/sbin:/sbin", systemPath)
}
