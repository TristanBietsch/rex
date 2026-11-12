package lifecycle

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

func exitCodeOf(t *testing.T, err error) int {
	t.Helper()
	var ec core.ExitCoder
	require.ErrorAs(t, err, &ec)
	return ec.ExitCode()
}

// Every daemon subcommand and reload validates its output flags first.
func TestDaemonSubcommandsRejectBadFlags(t *testing.T) {
	for _, args := range [][]string{
		{"start", "-bogus"}, {"stop", "-bogus"}, {"status", "-o", "yaml"}, {"restart", "-bogus"},
		{"logs", "-o", "json"},
	} {
		require.Equal(t, core.ExitInvalidArgs, exitCodeOf(t, RunDaemon(args)), "%v", args)
	}
	require.Equal(t, core.ExitInvalidArgs, exitCodeOf(t, RunReload([]string{"-o", "yaml"})))
}

func TestPrinterForUsesGivenWriters(t *testing.T) {
	var out, errw bytes.Buffer
	p := printerFor(&out, &errw)
	p.Warn("x")
	require.Contains(t, errw.String(), "x")
}
