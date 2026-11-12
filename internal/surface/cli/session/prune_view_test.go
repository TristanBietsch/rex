package session

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
	"github.com/tristanbietsch/rex/internal/testutil/golden"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestPruneViewsGolden(t *testing.T) {
	targets := []protocol.SessionSummary{
		{ID: "11110000", ShortID: "1111", Slug: "done-one", State: protocol.StateDone},
		{ID: "22220000", ShortID: "2222", Slug: "failed-one", State: protocol.StateFailed},
	}
	var secs []golden.Section
	for _, f := range []string{"text", "txt", "md", "json", "agent"} {
		run := func(name string, fn func(p *core.Printer) error) {
			out, _, err := actionRun(t, f, func() error { return fn(core.Out()) })
			require.NoError(t, err)
			secs = append(secs, golden.Section{Name: name + "." + f, Body: out})
		}
		run("dry", func(p *core.Printer) error { return emitPruneDryRun(p, targets, 1, " (1 still working kept)") })
		run("nothing", func(p *core.Printer) error { return emitPruneNothing(p, 0, "") })
		run("summary", func(p *core.Printer) error {
			return emitPruneSummary(p, pruneResult{cleared: 2, skipped: 1}, 1, " (1 still working kept)")
		})
	}
	golden.Check(t, "testdata/golden/prune.txtar", secs...)
}

// A declined question is exit 6, like rm; so is a prompt that cannot be
// asked (agent mode on a terminal).
func TestRunPrune_DeclineExits6(t *testing.T) {
	d := startPruneDaemon(t, true)
	withPruneIO(t, "n\n", true)
	err := RunPrune([]string{"-socket", d.sock})
	require.Equal(t, core.ExitOperationRefused, exitCode(t, err))
	require.ErrorContains(t, err, "not confirmed")
	require.Empty(t, d.Deleted())
}
