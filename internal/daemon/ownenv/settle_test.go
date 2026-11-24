package ownenv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
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
