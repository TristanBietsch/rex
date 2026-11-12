package registry

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// stubBinary writes an executable stub called name into dir. On Windows it
// is a .bat file, found through PATHEXT.
func stubBinary(t *testing.T, dir, name, body string) string {
	t.Helper()
	file := name
	content := "#!/bin/sh\n" + body + "\n"
	if runtime.GOOS == "windows" {
		file = name + ".bat"
		content = "@echo off\r\n" + body + "\r\n"
	}
	p := filepath.Join(dir, file)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o755)) //nolint:gosec // a test stub must be executable
	return p
}

func TestInstalled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	stubBinary(t, dir, "fake-agent", "exit 0")

	require.True(t, Installed(Tool{Command: []string{"fake-agent", "run"}}), "first word of command decides")
	require.False(t, Installed(Tool{Command: []string{"no-such-agent"}}))
	require.False(t, Installed(Tool{}), "no command is not installed")
	require.False(t, Installed(Tool{Command: []string{""}}))
}

// A directory or a non-executable file named like the binary is not an install.
func TestInstalledRejectsNonExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executability is by extension on Windows")
	}
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "plain"), []byte("x"), 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "adir"), 0o755)) //nolint:gosec // test dir
	require.False(t, Installed(Tool{Command: []string{"plain"}}))
	require.False(t, Installed(Tool{Command: []string{"adir"}}))
}

func TestRegistryInstalledMap(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	stubBinary(t, dir, "qwen", "exit 0")
	reg, err := Load("")
	require.NoError(t, err)
	got := reg.InstalledMap()
	require.True(t, got["qwen"])
	require.False(t, got["grok"])
	require.False(t, got["cursor"])
	require.Len(t, got, len(reg.Tools))
}
