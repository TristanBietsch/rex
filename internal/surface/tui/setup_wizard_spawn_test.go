package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

func spawnConfig(t *testing.T, body string) string {
	t.Helper()
	home := trustTestHome(t)
	cfg := filepath.Join(home, "config.yaml")
	if body != "" {
		require.NoError(t, os.WriteFile(cfg, []byte(body), 0o600))
	}
	return cfg
}

func loadSettings(t *testing.T, cfg string) *settings.Store {
	t.Helper()
	st := settings.NewStore()
	require.NoError(t, st.Load(cfg))
	return st
}

func TestApplyDefaults_WritesSpawnDefaults(t *testing.T) {
	cfg := spawnConfig(t, "")
	require.NoError(t, ApplyDefaultsNonInteractive(cfg, "", "antigravity", "claude-opus-4-6", "high", "", true))
	st := loadSettings(t, cfg)
	require.Equal(t, "antigravity", st.String("default_spawn_tool"))
	require.Equal(t, "claude-opus-4-6", st.String("default_spawn_model"))
	require.Equal(t, "high", st.String("default_spawn_effort"))
}

func TestApplyDefaults_ToolAloneRepairsModelAndEffort(t *testing.T) {
	cfg := spawnConfig(t, "default_spawn_tool: claude\ndefault_spawn_model: opus\ndefault_spawn_effort: max\n")
	require.NoError(t, ApplyDefaultsNonInteractive(cfg, "", "antigravity", "", "", "", true))
	st := loadSettings(t, cfg)
	require.Equal(t, "antigravity", st.String("default_spawn_tool"))
	require.Equal(t, "gemini-3.8-flash", st.String("default_spawn_model"))
	require.Equal(t, "high", st.String("default_spawn_effort"))
}

func TestApplyDefaults_KeepsOtherKeys(t *testing.T) {
	cfg := spawnConfig(t, "master_volume: 0.25\nmax_concurrent_sessions: 7\n")
	require.NoError(t, ApplyDefaultsNonInteractive(cfg, "", "claude", "sonnet", "low", "", true))
	st := loadSettings(t, cfg)
	require.Equal(t, 0.25, st.Get("master_volume"))
	require.EqualValues(t, 7, st.Get("max_concurrent_sessions"))
	require.Equal(t, "sonnet", st.String("default_spawn_model"))
}

func TestApplyDefaults_NoFlagsLeavesSpawnKeysAlone(t *testing.T) {
	cfg := spawnConfig(t, "default_spawn_model: haiku\n")
	require.NoError(t, ApplyDefaultsNonInteractive(cfg, "", "", "", "", "", true))
	st := loadSettings(t, cfg)
	require.Equal(t, "haiku", st.String("default_spawn_model"))
	require.Equal(t, settings.SourceDefault, st.Source("default_spawn_tool"))
}

func TestApplyDefaults_RejectsUnknownTool(t *testing.T) {
	cfg := spawnConfig(t, "master_volume: 0.25\n")
	before, err := os.ReadFile(cfg)
	require.NoError(t, err)

	err = ApplyDefaultsNonInteractive(cfg, "", "nope", "", "", "", true)
	require.ErrorContains(t, err, `unknown tool "nope" (valid: claude`)
	after, err := os.ReadFile(cfg)
	require.NoError(t, err)
	require.Equal(t, string(before), string(after), "nothing written")

	require.ErrorContains(t, ApplyDefaultsNonInteractive(cfg, "", "claude", "gpt-9", "", "", true), "unknown model")
	require.ErrorContains(t, ApplyDefaultsNonInteractive(cfg, "", "claude", "opus", "turbo", "", true), "unknown effort")
}

func TestSetupWizardWrite_KeepsMasterVolumeAndWritesSpawn(t *testing.T) {
	cfg := spawnConfig(t, "master_volume: 0.25\n")
	m := NewSetupWizardModel(cfg, "")
	m.defaultModel = "sonnet"
	msg, ok := m.doWriteCmd()().(setupWriteMsg)
	require.True(t, ok)
	require.NoError(t, msg.err)
	st := loadSettings(t, cfg)
	require.Equal(t, 0.25, st.Get("master_volume"))
	require.Equal(t, "claude", st.String("default_spawn_tool"))
	require.Equal(t, "sonnet", st.String("default_spawn_model"))
	require.Equal(t, "medium", st.String("default_spawn_effort"))
}

func TestSetupWizardWrite_RejectsUnknownModel(t *testing.T) {
	cfg := spawnConfig(t, "")
	m := NewSetupWizardModel(cfg, "")
	m.defaultModel = "gpt-9"
	msg, ok := m.doWriteCmd()().(setupWriteMsg)
	require.True(t, ok)
	require.ErrorContains(t, msg.err, `unknown model "gpt-9"`)
	_, statErr := os.Stat(cfg)
	require.True(t, os.IsNotExist(statErr), "nothing written")
}
