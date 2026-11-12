package boot

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// LogRegistryWarnings logs each non-fatal warning from loading reg (for
// example a dropped partial overlay of a retired tool) to the daemon log and
// stderr. Used at boot and on reload.
func LogRegistryWarnings(toolsPath string, reg *registry.Registry) {
	for _, w := range reg.Warnings {
		fmt.Fprintf(os.Stderr, "rex-daemon: registry warning: %s\n", w)
		slog.Warn("daemon: registry", "tools", toolsPath, "warning", w)
	}
}

// ReloadAll is the daemon's manual reload (SIGHUP and the Reload intent):
// tools.yaml into the server through cat (which then refreshes model lists in
// the background, ignoring a fresh cache), then config.yaml and config.lua
// into hub, whose subscribers hear of every setting that changed, then
// init.lua (luaHost, nil when Lua is off). Every layer is always attempted;
// the report lists what changed and what failed.
func ReloadAll(ctx context.Context, cat *Catalog, hub *SettingsHub, luaHost *LuaHost) protocol.ReloadReport {
	var rep protocol.ReloadReport
	if err := cat.Reload(ctx, hub.Current(), true); err != nil {
		rep.Errors = append(rep.Errors, protocol.FlattenDisplayText(err.Error()))
	} else {
		rep.Reloaded = append(rep.Reloaded, "tools.yaml")
	}
	hr, setErr := hub.ReloadReport()
	if setErr != nil {
		fmt.Fprintf(os.Stderr, "rex-daemon: %v\n", setErr)
		slog.Error("daemon: settings reload failed", "err", setErr)
		rep.Errors = append(rep.Errors, protocol.FlattenDisplayText(setErr.Error())+" (kept previous settings; skip_permissions and auto_trust_dirs are off until it parses)")
	} else {
		rep.Reloaded = append(rep.Reloaded, "config.yaml")
		slog.Info("daemon: settings reloaded", "changed", hr.Changed)
	}
	if settings.LuaConfigOn(hub.Current()) {
		if hr.LuaErr != nil {
			rep.Errors = append(rep.Errors, protocol.FlattenDisplayText(hr.LuaErr.Error())+" (kept its last good values)")
		} else {
			rep.Reloaded = append(rep.Reloaded, "config.lua")
		}
	}
	for _, w := range hr.Warnings {
		rep.Warnings = append(rep.Warnings, protocol.FlattenDisplayText(w))
	}
	rep.Changed = hr.Changed
	for _, id := range hr.Changed {
		if s, ok := settings.Find(id); ok && s.Apply == settings.ApplyDaemonRestart {
			rep.Restart = append(rep.Restart, id)
		}
	}
	if luaHost != nil && luaHost.Path() != "" {
		if err := luaHost.Reload(); err != nil { //nolint:contextcheck // init.lua loads under its own lua.DefaultLoadTimeout
			slog.Error("daemon: init.lua reload failed; previous handlers kept", "err", err)
			rep.Errors = append(rep.Errors, err.Error()+" (kept the previous handlers)")
		} else {
			rep.Reloaded = append(rep.Reloaded, "init.lua")
		}
	}
	return rep
}

// ReloadOnHUP calls reload (normally a ReloadAll) on every SIGHUP until ctx
// is canceled. The report is logged.
func ReloadOnHUP(ctx context.Context, reload func() protocol.ReloadReport) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP)
	defer signal.Stop(sig)
	for {
		select {
		case <-ctx.Done():
			return
		case <-sig:
			rep := reload()
			slog.Info("daemon: SIGHUP reload", "reloaded", rep.Reloaded, "changed", rep.Changed, "restart", rep.Restart, "errors", len(rep.Errors))
		}
	}
}
