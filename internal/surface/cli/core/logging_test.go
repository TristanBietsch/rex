package core

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// Importing core (which every CLI command does) must silence slog's default
// stderr handler, at every level.
func TestCLIDefaultLoggerDiscards(t *testing.T) {
	for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		require.False(t, slog.Default().Enabled(context.Background(), level), "slog default enabled at %s", level)
	}
}
