package session

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil/golden"
)

// `rex <cmd> -h` from the real FlagSets: Usage first, aligned flags, the
// output formats, one example.
func TestCommandHelpGolden(t *testing.T) {
	var secs []golden.Section
	for _, c := range []struct {
		name string
		run  func([]string) error
	}{{"ls", RunLs}, {"new", RunNew}, {"rm", RunRm}, {"resume", RunResume}} {
		for _, f := range []string{"text", "txt", "agent", "json"} {
			p, out := lsPrinter(t, f, 80, false)
			core.SetOut(p)
			t.Cleanup(func() { core.SetOut(nil) })
			require.ErrorIs(t, c.run([]string{"-h"}), core.ErrHelpShown)
			secs = append(secs, golden.Section{Name: c.name + "." + f, Body: out.String()})
		}
	}
	golden.Check(t, "testdata/golden/help.txtar", secs...)
}
