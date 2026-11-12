package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// trustWizard isolates HOME, makes ~/dev/proj (a checkout with a sub folder),
// starts in the sub folder and returns a describe-step wizard model with a
// store saved under the temp config path.
func spawnTrustWizard(t *testing.T) (m Model, home, proj, sub string) {
	t.Helper()
	home = t.TempDir()
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	proj = filepath.Join(home, "dev", "proj")
	sub = filepath.Join(proj, "pkg")
	require.NoError(t, os.MkdirAll(filepath.Join(proj, ".git"), 0o700))
	require.NoError(t, os.MkdirAll(sub, 0o700))
	t.Chdir(sub)
	m = wizardOn(&WizardState{Step: wizDescribe, Tools: wizTools()})
	m.Store = settings.NewStore()
	m.StorePath = filepath.Join(home, "config.yaml")
	return m, home, proj, sub
}

func ctrl(m Model, t tea.KeyType) Model { return sendKey(m, tea.KeyMsg{Type: t}) }

func launchReq(t *testing.T, m Model) (Model, protocol.NewSession, bool) {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	return wizardTrustRequest(m, cwd, m.Wizard.request(cwd, nil))
}

func TestWizardDescribe_CtrlTSendsTrustFolder(t *testing.T) {
	m, _, _, _ := spawnTrustWizard(t)
	_, req, ok := launchReq(t, m)
	require.True(t, ok)
	require.False(t, req.TrustFolder, "nothing is trusted unless the user acts")

	m = ctrl(m, tea.KeyCtrlT)
	require.Empty(t, m.Err)
	m, req, ok = launchReq(t, m)
	require.True(t, ok)
	require.True(t, req.TrustFolder)
	require.Empty(t, m.Store.String("auto_trust_dirs"), "per-session trust saves nothing")

	// A second press turns it off again.
	m = ctrl(m, tea.KeyCtrlT)
	_, req, _ = launchReq(t, m)
	require.False(t, req.TrustFolder)
}

func TestWizardDescribe_CtrlAAppendsAndTrusts(t *testing.T) {
	m, _, _, _ := spawnTrustWizard(t)
	m = ctrl(m, tea.KeyCtrlA)
	require.Empty(t, m.Err)
	require.Empty(t, m.Store.String("auto_trust_dirs"), "nothing is written until launch")

	m, req, ok := launchReq(t, m)
	require.True(t, ok)
	require.True(t, req.TrustFolder, "this session is trusted at once")
	require.Equal(t, "~/dev/proj", m.Store.String("auto_trust_dirs"), "the repo folder, not its parent")

	saved := settings.NewStore()
	require.NoError(t, saved.Load(m.StorePath))
	require.Equal(t, "~/dev/proj", saved.String("auto_trust_dirs"))
}

func TestWizardDescribe_LineShowsExactPaths(t *testing.T) {
	m, _, proj, sub := spawnTrustWizard(t)
	out := renderWizard(m)
	require.Contains(t, out, "folder not trusted")
	require.Contains(t, out, "ctrl+t")
	require.Contains(t, out, "ctrl+a")
	flat := strings.Join(strings.Fields(out), "")
	require.Contains(t, flat, sub, "ctrl+t trusts the folder the session starts in")
	require.Contains(t, flat, proj, "ctrl+a trusts the repo folder")
	require.NotContains(t, out, "trusted (auto_trust_dirs)")
}

func TestWizardDescribe_CoveredFolderShowsTrusted(t *testing.T) {
	m, _, proj, _ := spawnTrustWizard(t)
	require.NoError(t, m.Store.Set("auto_trust_dirs", proj))
	out := renderWizard(m)
	require.Contains(t, out, "trusted (auto_trust_dirs)")
	require.NotContains(t, out, "always trust")

	_, req, ok := launchReq(t, ctrl(m, tea.KeyCtrlT))
	require.True(t, ok)
	require.False(t, req.TrustFolder, "already covered: nothing to ask the daemon")
}

func TestWizardDescribe_HomeFolderCannotBeTrusted(t *testing.T) {
	m, home, _, _ := spawnTrustWizard(t)
	t.Chdir(home)
	out := renderWizard(m)
	require.Contains(t, out, "home folder: start rex in a project folder to trust it")

	m = ctrl(m, tea.KeyCtrlT)
	require.Contains(t, m.Err, "trust")
	_, req, _ := launchReq(t, m)
	require.False(t, req.TrustFolder)
	m = ctrl(m, tea.KeyCtrlA)
	require.Empty(t, m.Store.String("auto_trust_dirs"))
}

func TestWizardDescribe_CtrlAWithoutStoreFailsLaunch(t *testing.T) {
	m, _, _, _ := spawnTrustWizard(t)
	m.Store = nil
	m = ctrl(m, tea.KeyCtrlA)
	m, _, ok := launchReq(t, m)
	require.False(t, ok)
	require.Contains(t, m.Err, "trust")
}

func TestCommandTrust_AddsSuggestionForCwd(t *testing.T) {
	m, _, proj, _ := spawnTrustWizard(t)
	m.Wizard, m.Focus = nil, FocusBoard
	m, _ = executeCommand(m, "trust")
	require.Empty(t, m.Err)
	require.Equal(t, "~/dev/proj", m.Store.String("auto_trust_dirs"))
	require.Contains(t, m.Notice, proj)

	m, _ = executeCommand(m, "trust")
	require.Contains(t, m.Notice, "already trusted")
}

func TestCommandTrust_UsesSelectedSessionFolder(t *testing.T) {
	m, home, _, _ := spawnTrustWizard(t)
	other := filepath.Join(home, "dev", "other")
	require.NoError(t, os.MkdirAll(filepath.Join(other, ".git"), 0o700))
	m.Wizard, m.Focus = nil, FocusBoard
	m.Sessions = []protocol.SessionSummary{{ID: "a", Slug: "a", CWD: filepath.Join(other, "x")}}
	m.SelectedID = "a"
	m, _ = executeCommand(m, "trust")
	require.Empty(t, m.Err)
	require.Equal(t, "~/dev/other", m.Store.String("auto_trust_dirs"))
}

func TestCommandTrust_InvalidPathShowsError(t *testing.T) {
	m, home, _, _ := spawnTrustWizard(t)
	m.Wizard, m.Focus = nil, FocusBoard
	for _, arg := range []string{home, filepath.Dir(home), "rel/dir", "~/x/../y"} {
		m.Err, m.Notice = "", ""
		m, _ = executeCommand(m, "trust "+arg)
		require.Contains(t, m.Err, "trust:", arg)
		require.Empty(t, m.Store.String("auto_trust_dirs"), arg)
	}
	m, _ = executeCommand(m, "trust a b")
	require.Contains(t, m.Err, "usage")
}

func TestCommandTrust_InHomeNeedsAPath(t *testing.T) {
	m, home, _, _ := spawnTrustWizard(t)
	t.Chdir(home)
	m.Wizard, m.Focus = nil, FocusBoard
	m, _ = executeCommand(m, "trust")
	require.Contains(t, m.Err, "trust:")
	require.Empty(t, m.Store.String("auto_trust_dirs"))
}
