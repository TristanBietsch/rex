package session

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/surface/cli/core"
)

func requireInvalidArgs(t *testing.T, err error) core.ExitError {
	t.Helper()
	var ee core.ExitError
	require.True(t, errors.As(err, &ee), "%v", err)
	require.Equal(t, core.ExitInvalidArgs, ee.Code)
	return ee
}

func TestParseNewArgs_RelativeCwdAbsolutized(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(base, "rel", "dir"), 0o700))
	t.Chdir(base)
	got, err := resolveNewCwd(filepath.Join("rel", "dir"))
	require.NoError(t, err)
	want, err := filepath.EvalSymlinks(filepath.Join(base, "rel", "dir"))
	require.NoError(t, err)
	gotReal, err := filepath.EvalSymlinks(got)
	require.NoError(t, err)
	require.True(t, filepath.IsAbs(got), got)
	require.Equal(t, want, gotReal)
}

func TestParseNewArgs_EmptyCwdIsWorkingDir(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)
	got, err := resolveNewCwd("")
	require.NoError(t, err)
	require.Equal(t, wd, got)
}

func TestParseNewArgs_MissingCwd(t *testing.T) {
	_, err := resolveNewCwd(filepath.Join(t.TempDir(), "nope"))
	ee := requireInvalidArgs(t, err)
	require.Contains(t, ee.Msg, "-cwd")
	require.Contains(t, ee.Msg, "nope")
}

func TestParseNewArgs_CwdIsFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "f")
	require.NoError(t, os.WriteFile(f, nil, 0o600))
	ee := requireInvalidArgs(t, func() error { _, err := resolveNewCwd(f); return err }())
	require.Contains(t, ee.Msg, "not a folder")
}

func TestNewCwd_WindowsDriveRelative(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows paths only")
	}
	// \foo has no drive: Abs adds the current one instead of leaving it
	// relative to the daemon.
	_, err := resolveNewCwd(`\rex-test-missing`)
	_ = requireInvalidArgs(t, err)
}
