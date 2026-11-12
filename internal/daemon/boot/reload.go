package boot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/server"
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

// ReloadRegistry re-reads toolsPath and swaps it into srv. Used by SIGHUP and
// the Reload intent.
func ReloadRegistry(srv *server.Server, toolsPath string) error {
	reg, err := registry.Load(toolsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rex-daemon: reload failed: %v\n", err)
		slog.Error("daemon: reload failed", "tools", toolsPath, "err", err)
		return fmt.Errorf("reload %s: %w", toolsPath, err)
	}
	LogRegistryWarnings(toolsPath, reg)
	srv.SetRegistry(reg)
	fmt.Fprintln(os.Stderr, "rex-daemon: registry reloaded")
	slog.Info("daemon: reload ok", "tools", toolsPath, "count", len(reg.Tools))
	return nil
}

// ReloadAll is the daemon's manual reload (SIGHUP and the Reload intent):
// tools.yaml into srv, then config.yaml into hub, whose subscribers hear of
// every setting that changed. Both are always attempted; the error joins
// whichever failed.
func ReloadAll(srv *server.Server, toolsPath string, hub *SettingsHub) error {
	regErr := ReloadRegistry(srv, toolsPath)
	if setErr := hub.Reload(); setErr != nil {
		fmt.Fprintf(os.Stderr, "rex-daemon: %v\n", setErr)
		slog.Error("daemon: settings reload failed", "err", setErr)
		return errors.Join(regErr, setErr)
	}
	slog.Info("daemon: settings reloaded")
	return regErr
}

// ReloadOnHUP calls reload (normally a ReloadAll) on every SIGHUP until ctx
// is canceled. A failure is already logged by reload.
func ReloadOnHUP(ctx context.Context, reload func() error) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP)
	defer signal.Stop(sig)
	for {
		select {
		case <-ctx.Done():
			return
		case <-sig:
			_ = reload()
		}
	}
}
