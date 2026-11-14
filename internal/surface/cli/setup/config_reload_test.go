package setup

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/boot"
	"github.com/tristanbietsch/rex/internal/daemon/server"
	"github.com/tristanbietsch/rex/internal/testutil"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// hubDaemon starts a test daemon whose spawn-time reads and Reload intent go
// through a SettingsHub over the test HOME's config.yaml, as rex-daemon wires
// it. Nothing runs hub.Watch, so a change can only arrive by the intent.
func hubDaemon(t *testing.T) *boot.SettingsHub {
	t.Helper()
	st, path, err := settings.LoadDefault()
	require.NoError(t, err)
	hub := boot.NewSettingsHub(path, st)
	d := testutil.StartDaemonWithConfig(t, "", func(c *server.Config) {
		hub.WireSpawnReads(c, "")
		c.OnReload = func() protocol.ReloadReport {
			var rep protocol.ReloadReport
			if err := hub.Reload(); err != nil {
				rep.Errors = []string{err.Error()}
			}
			return rep
		}
	})
	t.Setenv("XDG_RUNTIME_DIR", filepath.Dir(d.Socket))
	return hub
}

// Turning a trust setting off through rex reaches the running daemon at
// once, with live_reload off and no poller: it is the Reload intent. A
// trust setting changes only at a terminal, so the test is one.
func TestConfigSetReset_TrustSettingsReloadTheDaemonAtOnce(t *testing.T) {
	atTerminal(t)
	require.NoError(t, configSet([]string{"live_reload", "false"}))
	require.NoError(t, configSet([]string{"skip_permissions", "true"}))
	dir := t.TempDir()
	require.NoError(t, configSet([]string{"auto_trust_dirs", dir}))
	hub := hubDaemon(t) // starts with both on
	require.True(t, boot.SkipPermissions(hub.Current()))
	require.Equal(t, []string{dir}, boot.AutoTrustDirs(hub.Current()))

	require.NoError(t, configSet([]string{"skip_permissions", "false"}))
	require.False(t, boot.SkipPermissions(hub.Current()), "set false: applied immediately")

	require.NoError(t, configReset([]string{"auto_trust_dirs"}))
	require.Empty(t, boot.AutoTrustDirs(hub.Current()), "reset: applied immediately")

	// Any other setting does not bother the daemon.
	require.NoError(t, configSet([]string{"summary_detail", "long"}))
	require.Equal(t, "medium", hub.Current().String("summary_detail"))
}

func TestConfigSet_TrustSettingWithDaemonDownStillSucceeds(t *testing.T) {
	atTerminal(t)
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir()) // nothing listens there
	require.NoError(t, configSet([]string{"skip_permissions", "true"}))
	require.NoError(t, configSet([]string{"skip_permissions", "false"}))
	require.NoError(t, configReset([]string{"skip_permissions"}))
}
