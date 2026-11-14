package inspect

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

func TestRunLog_NegativeN(t *testing.T) {
	for _, n := range []string{"-5", "-1"} {
		err := RunLog([]string{"-socket", "/nonexistent/rex.sock", "-n", n, "x"})
		var ee core.ExitError
		require.True(t, errors.As(err, &ee), "%v", err)
		require.Equal(t, core.ExitInvalidArgs, ee.Code)
		require.Contains(t, ee.Msg, "-n")
	}
}

func TestRunLog_ZeroNIsAll(t *testing.T) {
	err := RunLog([]string{"-socket", "/nonexistent/rex.sock", "-n", "0", "x"})
	var ee core.ExitError
	require.True(t, errors.As(err, &ee))
	require.Equal(t, core.ExitDaemonUnreachable, ee.Code, "-n 0 passes validation and reaches the socket")
}
