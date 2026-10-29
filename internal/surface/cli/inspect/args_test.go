package inspect

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// isolateDefaults points the default socket and state dirs at a temp dir so
// a command that ignored -socket could never reach the live daemon.
func isolateDefaults(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_RUNTIME_DIR", dir)
}

// Stray positionals are rejected instead of silently dropping the flags
// after them; flags after the selector parse.
func TestInspectCommandArity(t *testing.T) {
	isolateDefaults(t)
	dead := filepath.Join(t.TempDir(), "none.sock")
	cases := []struct {
		name string
		run  func([]string) error
		args []string
		want int
	}{
		{"status stray", RunStatus, []string{"x", "-json", "-socket", dead}, core.ExitInvalidArgs},
		{"status flags", RunStatus, []string{"-json", "-socket", dead}, core.ExitDaemonUnreachable},
		{"stats stray", RunStats, []string{"x", "-json"}, core.ExitInvalidArgs},
		{"digest stray", RunDigest, []string{"x", "-json"}, core.ExitInvalidArgs},
		{"render stray", RunRender, []string{"x", "-socket", dead}, core.ExitInvalidArgs},
		{"render bad width", RunRender, []string{"-w", "abc", "-socket", dead}, core.ExitInvalidArgs},
		{"render zero height", RunRender, []string{"-h", "0", "-socket", dead}, core.ExitInvalidArgs},
		{"render dial", RunRender, []string{"-w", "80", "-socket", dead}, core.ExitDaemonUnreachable},
		{"log flags after sel", RunLog, []string{"demo", "-f", "-n", "5", "-socket", dead}, core.ExitDaemonUnreachable},
		{"log extra", RunLog, []string{"demo", "x", "-socket", dead}, core.ExitInvalidArgs},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.run(c.args)
			var ee core.ExitError
			require.ErrorAs(t, err, &ee)
			require.Equal(t, c.want, ee.Code, ee.Msg)
		})
	}
}
