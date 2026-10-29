package rexlog

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
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

// Logs record session ids, paths and errors: owner-only, including a log an
// older build left world-readable.
func TestOpenLogFile_OwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	dir := filepath.Join(t.TempDir(), "logs")
	t.Setenv("REX_LOG_DIR", dir)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "old.log"), nil, 0o644))

	for _, name := range []string{"new", "old"} {
		f, err := openLogFile(name)
		require.NoError(t, err)
		require.NoError(t, f.Close())
		info, err := os.Stat(filepath.Join(dir, name+".log"))
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm(), name)
	}

	fresh := filepath.Join(t.TempDir(), "fresh")
	t.Setenv("REX_LOG_DIR", fresh)
	f, err := openLogFile("x")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	info, err := os.Stat(fresh)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
}

func TestInit_WritesToConfiguredDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REX_LOG_DIR", dir)

	Init("once-test")
	Close()

	data, err := os.ReadFile(filepath.Join(dir, "once-test.log"))
	require.NoError(t, err)
	require.True(t, strings.Contains(string(data), "rexlog: initialized"))
	require.Contains(t, string(data), "pid="+strconv.Itoa(os.Getpid()))
}

// Processes sharing a log file (two TUIs on tui.log) are told apart by pid.
func TestNewLogger_TagsPID(t *testing.T) {
	var buf bytes.Buffer
	newLogger(&buf).Info("hello", "k", "v")
	require.Contains(t, buf.String(), "pid="+strconv.Itoa(os.Getpid()))
	require.Contains(t, buf.String(), "k=v")
}
