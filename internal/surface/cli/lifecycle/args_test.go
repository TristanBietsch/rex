package lifecycle

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// Stray positionals are rejected. Only subcommands that cannot signal or
// spawn a real daemon on regression are exercised here.
func TestLifecycleCommandArity(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	cases := []struct {
		name string
		run  func([]string) error
		args []string
	}{
		{"daemon logs", RunDaemon, []string{"logs", "x", "-f"}},
		{"daemon status", RunDaemon, []string{"status", "x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.run(c.args)
			var ee core.ExitError
			require.ErrorAs(t, err, &ee)
			require.Equal(t, core.ExitInvalidArgs, ee.Code, ee.Msg)
		})
	}
}
