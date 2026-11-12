package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/settings/luacfg"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/wire/client"
	"github.com/tristanbietsch/rex/internal/workspace"
	"github.com/tristanbietsch/rex/internal/workspace/wsconfig"
)

// The daemon never applies a workspace's .rex/config.lua: started from inside
// a trusted workspace whose config.lua the CLI would apply, its settings are
// the user's own (config.yaml and ~/.config/rex/config.lua) and its layer
// stamp has no workspace part.
func TestRun_NeverAppliesTheWorkspaceLayer(t *testing.T) {
	home := t.TempDir()
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	luacfg.ResetMemo()
	t.Cleanup(func() { settings.SetLayer(nil); luacfg.ResetMemo() })

	cfgPath, err := paths.ConfigFile()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0o700))
	require.NoError(t, os.WriteFile(cfgPath, []byte(
		"workspaces: true\nlua_config: true\nsummary_enabled: false\nmodel_autodetect: false\nturn_snapshots: false\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(cfgPath), "config.lua"), []byte("rex.set('theme', 'noir')\n"), 0o600))

	root := filepath.Join(home, "proj")
	require.NoError(t, os.MkdirAll(root, 0o700))
	w, err := workspace.Init(root, "proj", nil)
	require.NoError(t, err)
	body := []byte("rex.set('theme', 'paper')\n")
	require.NoError(t, os.WriteFile(workspace.ConfigPath(root), body, 0o600))
	tp, err := paths.WorkspaceTrustFile()
	require.NoError(t, err)
	ts, err := workspace.OpenTrust(tp)
	require.NoError(t, err)
	require.NoError(t, ts.Trust(w, workspace.ConfigHash(body)))

	// The fixture is a workspace the CLI would apply.
	wsconfig.Install(root)
	s, _, err := settings.LoadDefault()
	require.NoError(t, err)
	require.Equal(t, "paper", s.Get("theme"), "a rex command in the workspace applies it")
	settings.SetLayer(nil)
	luacfg.ResetMemo()

	t.Chdir(root)
	dir, err := os.MkdirTemp("", "rexd")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	sock := filepath.Join(dir, "rex.sock")
	errc := make(chan error, 1)
	go func() {
		errc <- run([]string{"-socket", sock, "-state-dir", dir, "-tools", filepath.Join(dir, "tools.yaml")})
	}()
	var c *client.Client
	require.Eventually(t, func() bool {
		var derr error
		c, derr = client.Dial(sock)
		return derr == nil
	}, 10*time.Second, 20*time.Millisecond)

	// run installed this process's settings layer: the daemon's.
	s, rep, _, err := settings.LoadDefaultReport()
	require.NoError(t, err)
	require.Equal(t, "noir", s.Get("theme"), "the daemon keeps the user's own config.lua value")
	for id, at := range rep.SetAt {
		require.False(t, settings.WorkspaceSetAt(at), "%s set at %s", id, at)
	}
	require.NotContains(t, settings.LayerStamp(true), "ws:")
	require.NotContains(t, strings.Join(rep.Warnings, "\n"), ".rex", "the workspace file is not even looked at")

	require.NoError(t, c.Shutdown())
	select {
	case err := <-errc:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("daemon did not stop after Shutdown")
	}
	_ = c.Close()
}
