package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestValidateNewSession_RequiresCoreFields(t *testing.T) {
	err := validateNewSession(protocol.NewSession{})
	require.Error(t, err)

	err = validateNewSession(protocol.NewSession{
		ToolID: "echo", ModelID: "short", Slug: "x", CWD: t.TempDir(),
	})
	require.NoError(t, err)
}

// A relative cwd would be resolved against the daemon's own working
// directory, so it is refused.
func TestValidateNewSession_RelativeCwd(t *testing.T) {
	for _, cwd := range []string{"rel/dir", ".", "..", "./x", "~/dev"} {
		err := validateNewSession(protocol.NewSession{
			ToolID: "echo", ModelID: "short", Slug: "x", CWD: cwd,
		})
		require.ErrorContains(t, err, "absolute", cwd)
	}
}

func TestRequireSessionID(t *testing.T) {
	require.Error(t, requireSessionID(""))
	require.Error(t, requireSessionID("   "))
	require.NoError(t, requireSessionID("abc"))
}
