package ownenv

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"gopkg.in/yaml.v3"
)

// settleFiles returns a config.yaml path and a daemon.lock path in a temp dir.
// configYAML, when not empty, is written to the config; lock creates the lock.
func settleFiles(t *testing.T, configYAML string, lock bool) (cfg, lck string) {
	t.Helper()
	dir := t.TempDir()
	cfg = filepath.Join(dir, "config", "config.yaml")
	lck = filepath.Join(dir, "data", "daemon.lock")
	if configYAML != "" {
		require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
		require.NoError(t, os.WriteFile(cfg, []byte(configYAML), 0o600))
	}
	if lock {
		require.NoError(t, os.MkdirAll(filepath.Dir(lck), 0o700))
		require.NoError(t, os.WriteFile(lck, nil, 0o600))
	}
	return cfg, lck
}

func readYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, yaml.Unmarshal(raw, &m))
	return m
}

// An upgrade (a daemon ran here before) keeps the old behavior, and the
// other keys in the file survive.
func TestSettleChildEnv_UpgradeWithConfigPinsInherit(t *testing.T) {
	cfg, lck := settleFiles(t, "theme: dracula\n", true)
	got, err := SettleChildEnv(cfg, lck)
	require.NoError(t, err)
	require.Equal(t, "inherit", got)
	m := readYAML(t, cfg)
	require.Equal(t, "inherit", m["child_env"])
	require.Equal(t, "dracula", m["theme"])
}

// An upgrade by a user who never wrote a config still keeps the old behavior.
func TestSettleChildEnv_UpgradeWithoutConfigPinsInherit(t *testing.T) {
	cfg, lck := settleFiles(t, "", true)
	got, err := SettleChildEnv(cfg, lck)
	require.NoError(t, err)
	require.Equal(t, "inherit", got)
	require.Equal(t, "inherit", readYAML(t, cfg)["child_env"])
}

// A fresh install (no daemon ran yet, even if rex setup wrote a config) gets
// the allowlist, pinned so a later start does not mistake it for an upgrade.
func TestSettleChildEnv_FreshInstallPinsAllowlist(t *testing.T) {
	cfg, lck := settleFiles(t, "theme: dracula\n", false)
	got, err := SettleChildEnv(cfg, lck)
	require.NoError(t, err)
	require.Equal(t, "allowlist", got)
	require.Equal(t, "allowlist", readYAML(t, cfg)["child_env"])

	// The next start finds the lock and the key: nothing changes.
	require.NoError(t, os.MkdirAll(filepath.Dir(lck), 0o700))
	require.NoError(t, os.WriteFile(lck, nil, 0o600))
	got, err = SettleChildEnv(cfg, lck)
	require.NoError(t, err)
	require.Empty(t, got)
	require.Equal(t, "allowlist", readYAML(t, cfg)["child_env"])
}

// A value the user chose is never touched.
func TestSettleChildEnv_KeepsAnExplicitValue(t *testing.T) {
	cfg, lck := settleFiles(t, "child_env: allowlist\n", true)
	before, err := os.ReadFile(cfg)
	require.NoError(t, err)
	got, err := SettleChildEnv(cfg, lck)
	require.NoError(t, err)
	require.Empty(t, got)
	after, err := os.ReadFile(cfg)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

// An unparseable config is an error and is left as it is.
func TestSettleChildEnv_BadConfigIsLeftAlone(t *testing.T) {
	cfg, lck := settleFiles(t, "theme: [\n", true)
	got, err := SettleChildEnv(cfg, lck)
	require.Error(t, err)
	require.Empty(t, got)
	raw, err := os.ReadFile(cfg)
	require.NoError(t, err)
	require.Equal(t, "theme: [\n", string(raw))
}

// No config path (no home directory) does nothing.
func TestSettleChildEnv_NoPathIsANoop(t *testing.T) {
	got, err := SettleChildEnv("", "")
	require.NoError(t, err)
	require.Empty(t, got)
}

// A lock path that cannot be checked (its parent is a file) is an error, and
// nothing is written.
func TestSettleChildEnv_LockStatErrorIsReturned(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows reports a path under a file as not existing")
	}
	cfg, _ := settleFiles(t, "", false)
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	got, err := SettleChildEnv(cfg, filepath.Join(file, "daemon.lock"))
	require.Error(t, err)
	require.Empty(t, got)
	require.NoFileExists(t, cfg)
}

// A config folder that cannot be written is an error.
func TestSettleChildEnv_SaveErrorIsReturned(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs Unix permission bits and a non-root user")
	}
	dir := filepath.Join(t.TempDir(), "ro")
	require.NoError(t, os.MkdirAll(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // a directory needs its exec bit back so TempDir can empty it
	got, err := SettleChildEnv(filepath.Join(dir, "config.yaml"), "")
	require.Error(t, err)
	require.Empty(t, got)
}

// Without a home directory settle does nothing.
func TestSettle_NoHomeIsANoop(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	settle()
}

// settle uses the default paths: an upgrade pins inherit in ~/.config/rex,
// a bad config is reported and left alone, a fresh home pins allowlist.
func TestSettle_DefaultPaths(t *testing.T) {
	sandbox(t, "")
	cfg, err := paths.ConfigFile()
	require.NoError(t, err)
	lock, err := paths.DaemonLock()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(lock), 0o700))
	require.NoError(t, os.WriteFile(lock, nil, 0o600))
	settle()
	require.Equal(t, "inherit", readYAML(t, cfg)["child_env"])

	sandbox(t, "theme: [\n")
	cfg, err = paths.ConfigFile()
	require.NoError(t, err)
	settle()
	raw, err := os.ReadFile(cfg)
	require.NoError(t, err)
	require.Equal(t, "theme: [\n", string(raw))

	sandbox(t, "")
	cfg, err = paths.ConfigFile()
	require.NoError(t, err)
	settle()
	require.Equal(t, "allowlist", readYAML(t, cfg)["child_env"])
}
