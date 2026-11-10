package boot

import (
	"log/slog"
	"path/filepath"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// AutoTrustDirs reads auto_trust_dirs from config.yaml. The server calls it
// at each spawn, so a changed setting applies to the next session without a
// daemon restart. On a read error it trusts nothing.
func AutoTrustDirs() []string {
	st, _, err := settings.LoadDefault()
	if err != nil {
		slog.Warn("daemon: settings load failed for auto_trust_dirs; trusting no folder", "err", err)
		return nil
	}
	return filepath.SplitList(st.String("auto_trust_dirs"))
}
