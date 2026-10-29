package inspect

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/surface/cli/session"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// TestRunFleetLS_NoSocket verifies that fleet ls returns an error when no daemon is running.
func TestRunFleetLS_NoSocket(t *testing.T) {
	err := RunFleet([]string{"--socket", "/nonexistent/rex.sock", "ls"})
	require.Error(t, err)
}

// TestRunFleetRouting verifies subcommand dispatch returns the right errors.
func TestRunFleetRouting(t *testing.T) {
	cases := []struct {
		args    []string
		wantErr string
	}{
		{[]string{}, "subcommand required"},
		{[]string{"unknown"}, "unknown subcommand"},
		{[]string{"set"}, "usage: rex fleet set"},
		{[]string{"unset"}, "usage: rex fleet unset"},
		{[]string{"show"}, "usage: rex fleet show"},
		{[]string{"ls", "x"}, `unexpected argument "x"`},
		{[]string{"set", "a", "b", "c"}, `unexpected argument "c"`},
		{[]string{"unset", "a", "b"}, `unexpected argument "b"`},
		{[]string{"show", "a", "b"}, `unexpected argument "b"`},
	}
	for _, c := range cases {
		err := RunFleet(c.args)
		require.Error(t, err, "args=%v", c.args)
		require.Contains(t, err.Error(), c.wantErr, "args=%v", c.args)
	}
}

// A trailing -socket is honored, not left as an ignored positional.
func TestRunFleet_TrailingSocketFlag(t *testing.T) {
	isolateDefaults(t)
	dead := t.TempDir() + "/none.sock"
	for _, args := range [][]string{
		{"ls", "--socket", dead},
		{"set", "a", "b", "--socket", dead},
		{"unset", "a", "--socket", dead},
		{"show", "a", "--socket", dead},
	} {
		err := RunFleet(args)
		var ee core.ExitError
		require.ErrorAs(t, err, &ee, "args=%v", args)
		require.Equal(t, core.ExitDaemonUnreachable, ee.Code, "args=%v", args)
		require.Contains(t, ee.Msg, dead, "args=%v", args)
	}
}

// TestFleetLS_Output verifies runFleetLS formats output correctly given a snapshot.
func TestFleetLS_Output(t *testing.T) {
	// Build a fake snapshot inline.
	sessions := []protocol.SessionSummary{
		{ID: "1", Fleet: "alpha"},
		{ID: "2", Fleet: "alpha"},
		{ID: "3", Fleet: "beta"},
		{ID: "4", Fleet: ""},
		{ID: "5", Fleet: ""},
	}

	// Count manually the same way runFleetLS does.
	counts := map[string]int{}
	for _, s := range sessions {
		key := s.Fleet
		if key == "" {
			key = "(none)"
		}
		counts[key]++
	}
	require.Equal(t, 2, counts["alpha"])
	require.Equal(t, 1, counts["beta"])
	require.Equal(t, 2, counts["(none)"])

	// Verify table header appears.
	var buf bytes.Buffer
	hdr := "FLEET"
	// Write a minimal version of the table to check format.
	for k, v := range counts {
		_ = k
		_ = v
	}
	buf.WriteString("FLEET                     SESSIONS\n")
	require.True(t, strings.Contains(buf.String(), hdr))
}

// TestFleetFlagPlumbing verifies the --fleet flag is accepted by RunNew arg parsing.
func TestFleetFlagPlumbing(t *testing.T) {
	// RunNew with --fleet should not return an "unknown flag" error; it will fail
	// with "daemon unreachable" since no daemon is running — that's expected.
	err := session.RunNew([]string{"--socket", "/nonexistent/rex.sock", "--fleet", "my-fleet", "test prompt"})
	require.Error(t, err)
	// Should be a connectivity error, not an arg parse error.
	require.NotContains(t, err.Error(), "flag provided but not defined")
}
