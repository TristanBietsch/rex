package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/workspace"
	"github.com/tristanbietsch/rex/internal/workspace/wsconfig"
)

// wsLuaHome is luaConfigHome with workspaces on and a workspace ~/proj
// holding .rex/config.lua = wsLua, installed as this process's folder.
// trust says whether the workspace is trusted. It returns the root.
func wsLuaHome(t *testing.T, userLua, wsLua string, trust bool) string {
	t.Helper()
	luaConfigHome(t, userLua, "workspaces: true\n")
	home, err := paths.Home()
	require.NoError(t, err)
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	root := filepath.Join(home, "proj")
	require.NoError(t, os.MkdirAll(root, 0o700))
	w, err := workspace.Init(root, "proj", nil)
	require.NoError(t, err)
	if wsLua != "" {
		require.NoError(t, os.WriteFile(workspace.ConfigPath(root), []byte(wsLua), 0o600))
	}
	if trust {
		p, err := paths.WorkspaceTrustFile()
		require.NoError(t, err)
		ts, err := workspace.OpenTrust(p)
		require.NoError(t, err)
		require.NoError(t, ts.Trust(w, currentHash(t, w.Root)))
	}
	wsconfig.Install(root)
	t.Cleanup(func() { wsconfig.SetDir("") })
	return root
}

func TestConfigList_ShowsWorkspaceSource(t *testing.T) {
	root := wsLuaHome(t, "rex.set('spinner', 'moon')", "\nrex.set('theme', 'noir')", true)
	out, err := themeRun(t, "text", func() error { return configList(nil) })
	require.NoError(t, err)
	require.Contains(t, out, "workspace "+workspace.ConfigPath(root)+":2")
	require.Contains(t, out, "lua config.lua:1")

	out, err = themeRun(t, "json", func() error { return configList(nil) })
	require.NoError(t, err)
	require.Contains(t, out, `"set_at":"`+filepathJSON(workspace.ConfigPath(root))+`:2"`)
}

// filepathJSON escapes a path as JSON does (backslashes on Windows).
func filepathJSON(p string) string { return strings.ReplaceAll(p, `\`, `\\`) }

func TestConfigSet_WorkspaceKeyNamesTheWorkspaceFile(t *testing.T) {
	root := wsLuaHome(t, "", "rex.set('theme', 'noir')", true)
	err := configSet([]string{"theme", "paper"})
	require.Error(t, err)
	require.Contains(t, err.Error(), workspace.ConfigPath(root)+":1")
	require.Contains(t, err.Error(), "outside the workspace")
	require.NotContains(t, err.Error(), "config edit -lua")
}

func TestDoctor_WorkspaceConfigRow(t *testing.T) {
	root := wsLuaHome(t, "", "rex.set('theme', 'noir')", true)
	rows := checkWorkspaceConfig()
	require.Len(t, rows, 1)
	require.Equal(t, "workspace config.lua", rows[0].Name)
	require.Equal(t, checkPass, rows[0].Status)
	require.Contains(t, rows[0].Detail, workspace.ConfigPath(root))
	require.Contains(t, rows[0].Detail, "theme ("+workspace.ConfigPath(root)+":1)")

	wsLuaHome(t, "", "rex.set('theme', 'noir')", false)
	rows = checkWorkspaceConfig()
	require.Len(t, rows, 1)
	require.Equal(t, checkWarn, rows[0].Status)
	require.Contains(t, rows[0].Detail, "not trusted")
	require.Contains(t, rows[0].Detail, "rex ws trust")

	wsLuaHome(t, "", "rex.set('theme',", true)
	rows = checkWorkspaceConfig()
	require.Equal(t, checkWarn, rows[0].Status)
	require.Contains(t, rows[0].Detail, "trust settings read their defaults")

	wsLuaHome(t, "", "rex.set('skip_permissions', true)", true)
	rows = checkWorkspaceConfig()
	require.Equal(t, checkWarn, rows[0].Status)
	require.Contains(t, rows[0].Detail, "cannot be set")

	wsLuaHome(t, "", "", true)
	rows = checkWorkspaceConfig()
	require.Equal(t, checkInfo, rows[0].Status)
	require.Contains(t, rows[0].Detail, "none")

	wsconfig.SetDir("")
	require.Empty(t, checkWorkspaceConfig(), "outside a workspace: no row")

	luaConfigHome(t, "", "")
	wsconfig.Install(root)
	require.Empty(t, checkWorkspaceConfig(), "workspaces off: no row")
}

// currentHash is the hash of root's .rex/config.lua now ("" when none).
func currentHash(t *testing.T, root string) string {
	t.Helper()
	b, err := workspace.ReadConfig(root)
	if err != nil {
		return ""
	}
	return workspace.ConfigHash(b)
}
