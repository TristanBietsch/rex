package core

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveVersion(t *testing.T) {
	info := func(v string) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Main: debug.Module{Version: v}}, true
		}
	}
	none := func() (*debug.BuildInfo, bool) { return nil, false }

	require.Equal(t, "v1.2.3", resolveVersion("v1.2.3", info("v9.9.9")), "ldflags value wins")
	require.Equal(t, "v0.4.0", resolveVersion("dev", info("v0.4.0")), "go install @version")
	require.Equal(t, "v0.4.0", resolveVersion("", info("v0.4.0")))
	require.Equal(t, "dev", resolveVersion("dev", info("(devel)")))
	require.Equal(t, "dev", resolveVersion("dev", info("")))
	require.Equal(t, "dev", resolveVersion("dev", none))
}
