package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// failClosedStore writes auto_trust_dirs = ~/dev/other to the wizard's
// config.yaml and loads it under a config.lua that failed, as the TUI does:
// the effective value fails closed (empty).
func failClosedStore(t *testing.T, m Model) Model {
	t.Helper()
	require.NoError(t, os.WriteFile(m.StorePath, []byte("auto_trust_dirs: '~/dev/other'\n"), 0o600))
	settings.SetLayer(func() settings.LayerReport {
		return settings.LayerReport{Exists: true, Err: errors.New("config.lua: syntax error")}
	})
	t.Cleanup(func() { settings.SetLayer(nil) })
	st, _, err := settings.LoadLayered(m.StorePath)
	require.NoError(t, err)
	require.Empty(t, st.String("auto_trust_dirs"), "precondition: fails closed")
	m.Store = st
	return m
}

func savedTrustDirs(t *testing.T, path string) string {
	t.Helper()
	s := settings.NewStore()
	require.NoError(t, s.Load(path))
	return s.String("auto_trust_dirs")
}

// R05 review leftover: ctrl+a used to save just the new folder over the
// YAML list while config.lua was broken.
func TestWizardDescribe_CtrlAKeepsYAMLListWhileLuaIsBroken(t *testing.T) {
	m, home, _, _ := spawnTrustWizard(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, "dev", "other"), 0o700))
	m = failClosedStore(t, m)
	m = ctrl(m, tea.KeyCtrlA)
	_, _, ok := launchReq(t, m)
	require.True(t, ok)
	require.Equal(t, "~/dev/other"+string(filepath.ListSeparator)+"~/dev/proj", savedTrustDirs(t, m.StorePath))
}

func TestCommandTrust_KeepsYAMLListWhileLuaIsBroken(t *testing.T) {
	m, home, _, _ := spawnTrustWizard(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, "dev", "other"), 0o700))
	m = failClosedStore(t, m)
	m.Wizard, m.Focus = nil, FocusBoard
	m, _ = executeCommand(m, "trust")
	require.Empty(t, m.Err)
	require.Equal(t, "~/dev/other"+string(filepath.ListSeparator)+"~/dev/proj", savedTrustDirs(t, m.StorePath))
}
