package boot

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
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

// reloadOnSignal calls reload on every SIGHUP until ctx is canceled.
func reloadOnSignal(ctx context.Context, reload func()) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP)
	defer signal.Stop(sig)
	for {
		select {
		case <-ctx.Done():
			return
		case <-sig:
			reload()
		}
	}
}
