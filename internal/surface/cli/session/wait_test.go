package session

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Bad flags are refused before the socket is touched, so no daemon is needed.
func TestRunWait_BadUntil(t *testing.T) {
	for _, u := range []string{"bogus", "", "Done", "crashed", "complete"} {
		err := RunWait([]string{"-socket", "/nonexistent/rex.sock", "-until", u, "x"})
		ee := requireInvalidArgs(t, err)
		require.Contains(t, ee.Msg, "-until", u)
		require.Contains(t, ee.Msg, "working, needs_input, done, failed, any", u)
	}
}

func TestRunWait_ValidUntilReachesTheSocket(t *testing.T) {
	for _, u := range []string{"working", "needs_input", "done", "failed", "any"} {
		err := RunWait([]string{"-socket", "/nonexistent/rex.sock", "-until", u, "x"})
		require.Equal(t, 4, exitCode(t, err), u)
	}
}

func TestRunWait_NegativeTimeout(t *testing.T) {
	err := RunWait([]string{"-socket", "/nonexistent/rex.sock", "-timeout", "-1s", "x"})
	ee := requireInvalidArgs(t, err)
	require.Contains(t, ee.Msg, "-timeout")
}

func TestRunWait_FlagsAfterSelector(t *testing.T) {
	d := startPruneDaemon(t, true)
	err := RunWait([]string{"4444", "-socket", d.sock, "-until", "done", "-timeout", "100ms"})
	require.Equal(t, 7, exitCode(t, err), "flags after the selector are parsed; the session never finishes")
}

func TestRunWait_AlreadyThere(t *testing.T) {
	d := startPruneDaemon(t, true)
	require.NoError(t, RunWait([]string{"-socket", d.sock, "-until", "done", "1111"}))
}

func TestRunWait_TimeoutExit7(t *testing.T) {
	d := startPruneDaemon(t, true)
	err := RunWait([]string{"-socket", d.sock, "-until", "failed", "-timeout", "100ms", "4444"})
	require.Equal(t, 7, exitCode(t, err))
	require.ErrorContains(t, err, "timed out")
}
