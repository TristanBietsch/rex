package tui

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

func trustModel(t *testing.T, id string, reload func(context.Context, string) error) Model {
	t.Helper()
	return Model{
		Store:     settings.NewStore(),
		StorePath: filepath.Join(t.TempDir(), "config.yaml"),
		Socket:    "/tmp/rex-test/trust.sock",
		reloader:  reload,
		Settings:  &SettingsState{CursorID: id},
	}
}

func TestSettings_TrustSettingEditsReloadTheDaemon(t *testing.T) {
	var sockets []string
	m := trustModel(t, "skip_permissions", func(_ context.Context, s string) error { sockets = append(sockets, s); return nil })

	m = applySettingsAction(m, +1) // off -> on
	require.Equal(t, true, m.Store.Get("skip_permissions"))
	require.Equal(t, []string{"/tmp/rex-test/trust.sock"}, sockets, "saved, then the daemon told")

	m, _ = settingsKey(m, key("r")) // reset
	require.Equal(t, false, m.Store.Get("skip_permissions"))
	require.Len(t, sockets, 2)

	// Another setting leaves the daemon alone.
	m.Settings.CursorID = "mouse_enabled"
	_ = applySettingsAction(m, +1)
	require.Len(t, sockets, 2)
}

func TestSettings_TrustReloadFailureIsIgnored(t *testing.T) {
	m := trustModel(t, "skip_permissions", func(context.Context, string) error { return errors.New("boom") })
	m = applySettingsAction(m, +1)
	require.Equal(t, true, m.Store.Get("skip_permissions"), "the edit stands")
}

func TestReloadDaemonForTrust_OnlyTrustSettings(t *testing.T) {
	var n int
	f := func(context.Context, string) error { n++; return nil }
	reloadDaemonForTrust(f, "s", "usage")
	require.Zero(t, n)
	reloadDaemonForTrust(f, "s", "auto_trust_dirs")
	require.Equal(t, 1, n)
}
