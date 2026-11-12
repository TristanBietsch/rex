package meta

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// The scripts list every command of the command table, plus --version.
func TestRunCompletionListsEveryCommand(t *testing.T) {
	for _, sh := range []string{"bash", "zsh", "fish"} {
		var out bytes.Buffer
		env := core.Env{Getenv: func(string) string { return "" }, Width: 80, Scheme: "default", GOOS: "linux"}
		p, err := core.NewPrinter(core.FormatFlags{}, env, &out, io.Discard)
		require.NoError(t, err)
		core.SetOut(p)
		t.Cleanup(func() { core.SetOut(nil) })
		require.NoError(t, RunCompletion([]string{sh}))
		for _, c := range append(core.CommandNames(), "--version") {
			require.Contains(t, out.String(), c, sh)
		}
		require.NotContains(t, out.String(), "%!", sh+": no unfilled format verbs")
	}
}

func TestRunCompletionRejectsBadInput(t *testing.T) {
	for _, args := range [][]string{nil, {"tcsh"}, {"bash", "zsh"}, {"-o", "json", "bash"}} {
		var ec core.ExitCoder
		require.ErrorAs(t, RunCompletion(args), &ec, "%v", args)
		require.Equal(t, core.ExitInvalidArgs, ec.ExitCode(), "%v", args)
	}
}
