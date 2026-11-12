package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// bootHome points HOME at a temp dir and returns config.yaml's path and the
// themes directory.
func bootHome(t *testing.T) (cfg, dir string) {
	t.Helper()
	t.Cleanup(func() { applyTheme("default") })
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	dir = themesDir()
	require.NotEmpty(t, dir)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	return filepath.Join(filepath.Dir(dir), "config.yaml"), dir
}

func bootThemeStep(t *testing.T, m Model) (Model, bootLine) {
	t.Helper()
	msg := stepThemesLoad(m)()
	loaded, ok := msg.(themesLoadedMsg)
	require.True(t, ok, "got %T", msg)
	next, _ := m.Update(loaded)
	nm := next.(Model)
	return nm, nm.BootLog[len(nm.BootLog)-1]
}

func TestBootLoadsUserThemesThenAppliesTheConfiguredOne(t *testing.T) {
	cfg, dir := bootHome(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mine.yaml"), []byte("extends: noir\ncolors: {accent: \"#FF7A59\"}\nglyphs: {needs: \"!\"}\n"), 0o600))
	store := settings.NewStore()
	require.NoError(t, store.Set("theme", "mine"))

	m, step := bootThemeStep(t, Model{Store: store, StorePath: cfg})
	require.Equal(t, "themes.load", step.Name)
	require.Equal(t, stepOK, step.Status)
	require.Contains(t, step.Desc, "mine")
	require.Equal(t, []string{"mine"}, m.Themes.UserNames())
	require.NotEmpty(t, m.configStamp, "live reload starts from the boot state")

	next, _ := m.Update(stepThemeApply(m)())
	log := next.(Model).BootLog
	require.Equal(t, "theme.apply", log[len(log)-1].Name)
	require.Equal(t, stepOK, log[len(log)-1].Status)
	require.Equal(t, lipgloss.Color("#FF7A59"), colorAccent)
	require.Equal(t, "!", stateGlyph("needs_input"))
}

func TestBootWithNoThemeFilesSkips(t *testing.T) {
	cfg, _ := bootHome(t)
	_, step := bootThemeStep(t, Model{Store: settings.NewStore(), StorePath: cfg})
	require.Equal(t, stepSkip, step.Status)
	require.Equal(t, "no user themes", step.Desc)
}

func TestBootBadThemeFileWarnsAndTheTUIStillStarts(t *testing.T) {
	cfg, dir := bootHome(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("colors: {accent: \"red\"}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ok.yaml"), []byte("extends: paper"), 0o600))
	store := settings.NewStore()
	require.NoError(t, store.Set("theme", "bad"))

	m, step := bootThemeStep(t, Model{Store: store, StorePath: cfg})
	require.Equal(t, stepWarn, step.Status)
	require.Contains(t, step.Desc, "1 loaded, 1 skipped: bad.yaml")
	require.Equal(t, []string{"ok"}, m.Themes.UserNames())

	next, _ := m.Update(stepThemeApply(m)())
	log := next.(Model).BootLog
	last := log[len(log)-1]
	require.Equal(t, stepWarn, last.Status)
	require.Contains(t, last.Desc, `theme "bad" not loaded`)
	require.Contains(t, last.Desc, "colors.accent")
	require.Equal(t, lipgloss.Color("#B392F0"), colorAccent, "the default palette is in force")
}

func TestWizardOffersUserThemesAndKeepsAnUnavailableOne(t *testing.T) {
	cfg, dir := bootHome(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mine.yaml"), []byte("extends: noir"), 0o600))
	require.NoError(t, os.WriteFile(cfg, []byte("theme: gone\n"), 0o600))

	w := NewSetupWizardModel(cfg, filepath.Join(dir, "sock"))
	require.Equal(t, append(enumOptions("theme"), "mine", "gone"), w.themeOptions)
	require.Equal(t, "gone", w.themeOptions[w.themeIdx], "an unavailable configured theme is not reset")

	require.NoError(t, os.WriteFile(cfg, []byte("theme: mine\n"), 0o600))
	w = NewSetupWizardModel(cfg, filepath.Join(dir, "sock"))
	require.Equal(t, append(enumOptions("theme"), "mine"), w.themeOptions)
	require.Equal(t, "mine", w.themeOptions[w.themeIdx])
}
