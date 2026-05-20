package boot

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/server"
)

// ReloadOnHUP listens for SIGHUP and swaps in a freshly-loaded registry.
func ReloadOnHUP(ctx context.Context, srv *server.Server, toolsPath string) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGHUP)
	defer signal.Stop(sig)
	for {
		select {
		case <-ctx.Done():
			return
		case <-sig:
			reg, err := registry.Load(toolsPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "rex-daemon: reload failed: %v\n", err)
				slog.Error("daemon: SIGHUP reload failed", "tools", toolsPath, "err", err)
				continue
			}
			srv.SetRegistry(reg)
			fmt.Fprintln(os.Stderr, "rex-daemon: registry reloaded")
			slog.Info("daemon: SIGHUP reload ok", "tools", toolsPath, "count", len(reg.Tools))
		}
	}
}
