package tui

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// spawnTrustWizard isolates HOME, makes ~/dev/proj (a checkout with a sub
// folder), starts in the sub folder and returns a model with the new-agent
// page open and a store saved under the temp config path.
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
	m = boardModel()
	m.Width, m.Height = 120, 40
	m.DaemonTools, m.toolsKnown = daemonTools(t, "claude", "codex"), true
	m.Store = settings.NewStore()
	m.StorePath = filepath.Join(home, "config.yaml")
	m.reloader = func(context.Context, string) error { return nil } // never a real daemon
	m = naOpen(t, m)
	return m, home, proj, sub
}

// trustText is the page's trust step body as plain text.
func trustText(m Model) string {
	return ansi.Strip(strings.Join(naTrustBody(m, m.NewAgent, 78, 40), "\n"))
}

func ctrl(m Model, t tea.KeyType) Model { return sendKey(m, tea.KeyMsg{Type: t}) }

func launchReq(t *testing.T, m Model) (Model, protocol.NewSession, bool) {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	req, err := m.NewAgent.request(cwd, nil)
	require.NoError(t, err)
	return newAgentTrustRequest(m, cwd, req)
}

func TestNewAgentTrust_CtrlTSendsTrustFolder(t *testing.T) {
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

func TestNewAgentTrust_CtrlAAppendsAndTrusts(t *testing.T) {
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

func TestNewAgentTrust_LineShowsExactPaths(t *testing.T) {
	m, _, proj, sub := spawnTrustWizard(t)
	out := trustText(m)
	require.Contains(t, out, "ask: the agent will ask")
	require.Contains(t, out, "this session only")
	require.Contains(t, out, "always: saves ~/dev/proj to auto_trust_dirs")
	flat := strings.Join(strings.Fields(out), "")
	// The folders are shown slashed (wrapPath breaks them at "/").
	require.Contains(t, flat, filepath.ToSlash(sub), "this session trusts the folder it starts in")
	require.Contains(t, flat, filepath.ToSlash(proj), "always trusts the repo folder")
	require.NotContains(t, out, "trusted (auto_trust_dirs)")
}

func TestNewAgentTrust_CoveredFolderShowsTrusted(t *testing.T) {
	m, _, proj, _ := spawnTrustWizard(t)
	require.NoError(t, m.Store.Set("auto_trust_dirs", proj))
	out := trustText(m)
	require.Contains(t, out, "trusted (auto_trust_dirs)")
	require.NotContains(t, out, "always:")

	_, req, ok := launchReq(t, ctrl(m, tea.KeyCtrlT))
	require.True(t, ok)
	require.False(t, req.TrustFolder, "already covered: nothing to ask the daemon")
}

func TestNewAgentTrust_HomeFolderCannotBeTrusted(t *testing.T) {
	m, home, _, _ := spawnTrustWizard(t)
	t.Chdir(home)
	out := trustText(m)
	require.Contains(t, out, "home folder: start rex in a project folder to trust it")

	m = ctrl(m, tea.KeyCtrlT)
	require.Contains(t, m.NewAgent.Err, "trust")
	_, req, _ := launchReq(t, m)
	require.False(t, req.TrustFolder)
	m = ctrl(m, tea.KeyCtrlA)
	require.Empty(t, m.Store.String("auto_trust_dirs"))
}

func TestNewAgentTrust_CtrlAWithoutStoreFailsLaunch(t *testing.T) {
	m, _, _, _ := spawnTrustWizard(t)
	m.Store = nil
	m = ctrl(m, tea.KeyCtrlA)
	m, _, ok := launchReq(t, m)
	require.False(t, ok)
	require.Contains(t, m.Err, "trust")
}

func TestCommandTrust_AddsSuggestionForCwd(t *testing.T) {
	m, _, proj, _ := spawnTrustWizard(t)
	m.NewAgent, m.Focus = nil, FocusBoard
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
	m.NewAgent, m.Focus = nil, FocusBoard
	m.Sessions = []protocol.SessionSummary{{ID: "a", Slug: "a", CWD: filepath.Join(other, "x")}}
	m.SelectedID = "a"
	m, _ = executeCommand(m, "trust")
	require.Empty(t, m.Err)
	require.Equal(t, "~/dev/other", m.Store.String("auto_trust_dirs"))
}

func TestCommandTrust_InvalidPathShowsError(t *testing.T) {
	m, home, _, _ := spawnTrustWizard(t)
	m.NewAgent, m.Focus = nil, FocusBoard
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
	m.NewAgent, m.Focus = nil, FocusBoard
	m, _ = executeCommand(m, "trust")
	require.Contains(t, m.Err, "trust:")
	require.Empty(t, m.Store.String("auto_trust_dirs"))
}

// A pasted path can carry escapes or other control characters; :trust drops
// them, as the setup wizard's folder field does, so they never reach
// config.yaml or the status line.
func TestCommandTrust_StripsControlCharacters(t *testing.T) {
	m, _, proj, _ := spawnTrustWizard(t)
	m.NewAgent, m.Focus = nil, FocusBoard
	m, _ = executeCommand(m, "trust \x1b[31m~/dev/\x07proj\x1b[0m")
	require.Empty(t, m.Err)
	require.Equal(t, "~/dev/proj", m.Store.String("auto_trust_dirs"))
	require.Contains(t, m.Notice, proj)
	require.NotContains(t, m.Notice, "\x1b")
}

// On a workspace board a session starts at the workspace root, so the trust
// line offers (and ctrl+a saves) the workspace's repo folder, not the folder
// the TUI was started in.
func TestNewAgentTrust_WorkspaceBoardTrustsWorkspaceRoot(t *testing.T) {
	m, home, proj, _ := spawnTrustWizard(t)
	ws := filepath.Join(home, "dev", "other")
	require.NoError(t, os.MkdirAll(filepath.Join(ws, ".git"), 0o700))
	require.NoError(t, m.Store.Set("workspaces", true))
	m.WS = &WorkspaceScope{ID: "w1", Name: "other", Root: ws}

	var flat strings.Builder // long paths wrap; join the pieces back up
	for _, l := range naTrustBody(m, m.NewAgent, 78, 40) {
		flat.WriteString(strings.TrimSpace(l))
	}
	lines := flat.String()
	require.Contains(t, lines, filepath.ToSlash(ws))
	require.NotContains(t, lines, filepath.ToSlash(proj))

	m = ctrl(m, tea.KeyCtrlA)
	require.Empty(t, m.Err)
	req0, err := m.NewAgent.request(spawnCWD(m), nil)
	require.NoError(t, err)
	m, req, ok := newAgentTrustRequest(m, spawnCWD(m), req0)
	require.True(t, ok)
	require.True(t, req.TrustFolder)
	require.Equal(t, ws, req.CWD)
	require.Equal(t, "~/dev/other", m.Store.String("auto_trust_dirs"))
}

// A folder name is not trusted text: a checkout can hold a folder whose name
// carries an escape sequence. The trust line must not send it raw to the
// terminal.
func TestNewAgentTrust_TrustLineFlattensFolderName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("control characters are not legal in Windows folder names")
	}
	m, _, proj, _ := spawnTrustWizard(t)
	evil := filepath.Join(proj, "x\x1b]0;pwned\x07y")
	require.NoError(t, os.MkdirAll(evil, 0o700))
	t.Chdir(evil)
	out := trustText(m)
	require.NotContains(t, out, "\x1b]0;", "an OSC from a folder name reaches the terminal")
	require.NotContains(t, out, "\x07")
}

// A saved trust folder must reach the running daemon at once, as a Settings
// page edit does: with live_reload off it would otherwise never apply, while
// the notice says new sessions skip the dialog.
func TestTrustSave_TellsDaemonToReload(t *testing.T) {
	m, _, _, _ := spawnTrustWizard(t)
	m.Socket = "/tmp/rex-test.sock"
	var got []string
	m.reloader = func(_ context.Context, socket string) error { got = append(got, socket); return nil }

	board := m
	board.NewAgent, board.Focus = nil, FocusBoard
	board, _ = executeCommand(board, "trust")
	require.Empty(t, board.Err)
	require.Equal(t, []string{"/tmp/rex-test.sock"}, got, ":trust reloads the daemon")

	got = nil
	m.Store = settings.NewStore()
	m = ctrl(m, tea.KeyCtrlA)
	_, _, ok := launchReq(t, m)
	require.True(t, ok)
	require.Equal(t, []string{"/tmp/rex-test.sock"}, got, "ctrl+a reloads the daemon")
}
