package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

// render is absent: it defines -h as the render height.
var helpCommands = []string{
	"status", "ls", "new", "attach", "reply", "send", "log", "wait", "rm",
	"prune", "rename", "name", "done", "undone", "archive", "complete",
	"reload", "completion", "setup", "doctor", "update", "uninstall",
	"digest", "stats", "fleet",
}

func TestRun_HelpFlagExitsZero(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, cmd := range helpCommands {
		t.Run(cmd, func(t *testing.T) {
			err := run([]string{cmd, "-h"})
			require.ErrorIs(t, err, core.ErrHelpShown)
			require.Equal(t, 0, exitCodeFor(err))
			require.Empty(t, err.Error(), "main prints nothing for an empty message")
		})
	}
}
