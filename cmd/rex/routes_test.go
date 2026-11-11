package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// Commands that need no daemon run through the dispatcher and print in the
// requested format.
func TestRun_OfflineCommandRoutes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("REX_LOG_DIR", t.TempDir())
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"version"}, "v1\n"},
		{[]string{"--version"}, "v1\n"},
		{[]string{"-v"}, "v1\n"},
		{[]string{"errors", "-o", "agent"}, "ok cmd=errors count=0\n"},
		{[]string{"config", "get", "spinner"}, "braille\n"},
		{[]string{"update", "-check", "-o", "agent"}, "ok cmd=update version=v1 remote=\"\"\n"},
	} {
		out := capturePrinter(t, "txt")
		require.NoError(t, run(tc.args), tc.args)
		require.Equal(t, tc.want, out.String(), tc.args)
	}
}

// A closed log: a failed command is recorded, and the exit code is the
// error's.
func TestRun_ErrorsAreRecorded(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	t.Setenv("REX_LOG_DIR", dir)
	capturePrinter(t, "txt")
	err := run([]string{"frobnicate"})
	require.Equal(t, core.ExitInvalidArgs, exitCodeFor(err))
	core.RecordError([]string{"frobnicate"}, err, core.ErrLogOptions{})

	out := capturePrinter(t, "txt")
	require.NoError(t, run([]string{"errors"}))
	require.Contains(t, out.String(), "frobnicate")
}
