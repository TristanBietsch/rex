package tui

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/runtime/daemonctl"
)

// trustReloadTimeout bounds how long a save waits for the daemon to answer
// the Reload intent, so a hung daemon cannot stall the screen.
const trustReloadTimeout = time.Second

// trustReload is the Reload intent sent after a trust setting is saved. Tests
// swap it so no real daemon is reached.
var trustReload = daemonctl.ReloadDaemon

// reloadDaemonForTrust tells the daemon on socket to re-read config.yaml
// right after rex saved id, when id is a trust setting (skip_permissions,
// auto_trust_dirs), so revoking one applies at once instead of at the daemon's
// next poll. Best effort: no daemon running is fine (it reads the file at
// start) and a failure never reaches the user.
func reloadDaemonForTrust(reload func(context.Context, string) error, socket, id string) {
	if !settings.IsTrustSetting(id) {
		return
	}
	if reload == nil {
		reload = trustReload
	}
	ctx, cancel := context.WithTimeout(context.Background(), trustReloadTimeout)
	defer cancel()
	if err := reload(ctx, socket); err != nil && !errors.Is(err, daemonctl.ErrNotRunning) {
		slog.Warn("tui: could not tell the daemon to reload (it applies the change within 5 s)", "id", id, "err", err)
	}
}
