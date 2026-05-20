package rexlog

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLevelFromEnv(t *testing.T) {
	tests := []struct {
		env  string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"WARN", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},
		{"nonsense", slog.LevelInfo},
	}
	for _, tc := range tests {
		t.Run(tc.env, func(t *testing.T) {
			t.Setenv("REX_LOG_LEVEL", tc.env)
			require.Equal(t, tc.want, levelFromEnv())
		})
	}
}

func TestStateDir_UsesRexLogDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REX_LOG_DIR", dir)
	require.Equal(t, dir, stateDir())
}

func TestOpenLogFile_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REX_LOG_DIR", dir)

	f, err := openLogFile("testbin")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	info, err := os.Stat(filepath.Join(dir, "testbin.log"))
	require.NoError(t, err)
	require.False(t, info.IsDir())
}

func TestInit_WritesToConfiguredDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REX_LOG_DIR", dir)

	Init("once-test")
	Close()

	data, err := os.ReadFile(filepath.Join(dir, "once-test.log"))
	require.NoError(t, err)
	require.True(t, strings.Contains(string(data), "rexlog: initialized"))
}
