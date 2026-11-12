package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

func TestRun_QuietBootTakesNoArguments(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	capturePrinter(t, "txt")
	require.Equal(t, core.ExitInvalidArgs, exitCodeFor(run([]string{"--quiet-boot", "x"})))
}
