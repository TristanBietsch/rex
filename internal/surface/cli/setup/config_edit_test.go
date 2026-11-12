//go:build !windows

package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// argsEditor writes a shell script that records its arguments, one per
// line, and returns its path and the record file.
func argsEditor(t *testing.T) (script, record string) {
	t.Helper()
	dir := t.TempDir()
	record = filepath.Join(dir, "args")
	script = filepath.Join(dir, "ed")
	body := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\" >> '" + record + "'; done\n"
	require.NoError(t, os.WriteFile(script, []byte(body), 0o700)) //nolint:gosec // G306: the test editor must be executable.
	return script, record
}

// $EDITOR may carry arguments ("code -w"); it used to be run as one
// program name and failed. rex config edit also opens the init.lua the
// daemon loads (lua_config_path), not always the default.
func TestConfigEdit_EditorArgsAndLuaConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	custom := filepath.Join(home, "scripts", "rex.lua")
	cfgDir := filepath.Join(home, ".config", "rex")
	require.NoError(t, os.MkdirAll(cfgDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte("lua_config_path: ~/scripts/rex.lua\n"), 0o600))

	script, record := argsEditor(t)
	t.Setenv("EDITOR", script+" -w")
	require.NoError(t, configEdit(nil))

	got, err := os.ReadFile(record)
	require.NoError(t, err)
	require.Equal(t, []string{"-w", custom}, strings.Fields(string(got)))
}
