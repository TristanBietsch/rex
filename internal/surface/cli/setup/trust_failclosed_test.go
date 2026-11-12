package setup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
)

// brokenLua installs a config.lua layer that failed to evaluate, so
// auto_trust_dirs fails closed (reads empty) in this process.
func brokenLua(t *testing.T) {
	t.Helper()
	settings.SetLayer(func() settings.LayerReport {
		return settings.LayerReport{Exists: true, Err: errors.New("config.lua: syntax error")}
	})
	t.Cleanup(func() { settings.SetLayer(nil) })
}

func writeTrustYAML(t *testing.T, value string) string {
	t.Helper()
	cfg, err := paths.ConfigFile()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfg), 0o700))
	require.NoError(t, os.WriteFile(cfg, []byte("auto_trust_dirs: '"+value+"'\n"), 0o600))
	return cfg
}

func yamlTrustDirs(t *testing.T) string {
	t.Helper()
	cfg, err := paths.ConfigFile()
	require.NoError(t, err)
	s := settings.NewStore()
	require.NoError(t, s.Load(cfg))
	return s.String("auto_trust_dirs")
}

// R05 review leftover: with config.lua broken the effective value is empty,
// and rex trust used to save just the new folder over the YAML list.
func TestRunTrust_AddKeepsYAMLListWhileLuaIsBroken(t *testing.T) {
	home, project := trustHome(t)
	other := filepath.Join(home, "dev", "other")
	require.NoError(t, os.MkdirAll(other, 0o700))
	writeTrustYAML(t, "~/dev/other")
	brokenLua(t)

	trustAddOK(t, project)
	require.Equal(t, "~/dev/other"+string(filepath.ListSeparator)+project, yamlTrustDirs(t),
		"the YAML entries survive; only the new folder is added")
}

func TestRunTrust_RemoveEditsYAMLListWhileLuaIsBroken(t *testing.T) {
	home, project := trustHome(t)
	other := filepath.Join(home, "dev", "other")
	require.NoError(t, os.MkdirAll(other, 0o700))
	writeTrustYAML(t, "~/dev/other"+string(filepath.ListSeparator)+project)
	brokenLua(t)

	d := runTrustJSON(t, "-rm", project)
	require.True(t, d.Changed, "the YAML list held the folder")
	require.Equal(t, "~/dev/other", yamlTrustDirs(t))
}
