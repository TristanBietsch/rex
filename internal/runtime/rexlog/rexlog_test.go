package rexlog

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
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

func TestPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	t.Setenv("REX_LOG_DIR", "")
	got, err := Path("tui")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(paths.ForHome(home).LogDir, "tui.log"), got)

	dir := t.TempDir()
	t.Setenv("REX_LOG_DIR", dir)
	got, err = Path("tui")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "tui.log"), got)

	// REX_LOG_DIR needs no home; the default dir does.
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	got, err = Path("tui")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "tui.log"), got)
	t.Setenv("REX_LOG_DIR", "")
	_, err = Path("tui")
	require.ErrorIs(t, err, paths.ErrNoHome)
	_, err = openLogFile("tui")
	require.ErrorIs(t, err, paths.ErrNoHome)
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

// A regular file where a directory belongs makes every open fail cleanly.
func TestOpenLogFile_ErrorsWhenDirIsAFile(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))

	t.Setenv("REX_LOG_DIR", filepath.Join(blocker, "logs"))
	_, err := openLogFile("x")
	require.Error(t, err)

	t.Setenv("REX_LOG_DIR", "")
	t.Setenv("HOME", blocker)
	t.Setenv("USERPROFILE", blocker)
	_, err = openLogFile("x")
	require.Error(t, err)

	require.Error(t, PrivateDir(filepath.Join(blocker, "sub")))
	_, err = OpenPrivate(filepath.Join(blocker, "sub", "x.log"))
	require.Error(t, err)
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
