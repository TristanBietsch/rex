package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

func TestExitCodeFor(t *testing.T) {
	refused := core.NewExitError(core.ExitOperationRefused, "no")
	require.Equal(t, core.ExitOperationRefused, exitCodeFor(refused))
	require.Equal(t, core.ExitOperationRefused, exitCodeFor(fmt.Errorf("update: %w", refused)), "wrapped code survives")
	require.Equal(t, core.ExitGeneric, exitCodeFor(errors.New("plain")))
}

func TestRun_UnknownCommandIsInvalidArgs(t *testing.T) {
	err := run([]string{"bogus"})
	require.Error(t, err)
	require.Equal(t, core.ExitInvalidArgs, exitCodeFor(err))
	require.Contains(t, err.Error(), `unknown command "bogus"`)
}
