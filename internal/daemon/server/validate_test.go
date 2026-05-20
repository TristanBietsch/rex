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
		ToolID: "echo", ModelID: "short", Slug: "x", CWD: "/tmp",
	})
	require.NoError(t, err)
}

func TestRequireSessionID(t *testing.T) {
	require.Error(t, requireSessionID(""))
	require.Error(t, requireSessionID("   "))
	require.NoError(t, requireSessionID("abc"))
}
