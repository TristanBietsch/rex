package boot

import (
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// AutoTrustDirs reads auto_trust_dirs from config.yaml. The server calls it
// at each spawn, so a changed setting applies to the next session without a
// daemon restart. On a read error it trusts nothing.
//
// Entries are split on the OS list separator (":" on Unix), which is also
// legal inside a folder name: "/work/a:b" would split into "/work/a" (too
// broad) and "b". So every piece must be absolute or start with "~/";
// otherwise the whole setting is ignored rather than guessed at.
func AutoTrustDirs() []string {
	st, _, err := settings.LoadDefault()
	if err != nil {
		slog.Warn("daemon: settings load failed for auto_trust_dirs; trusting no folder", "err", err)
		return nil
	}
	dirs := filepath.SplitList(st.String("auto_trust_dirs"))
	for _, d := range dirs {
		d = strings.TrimSpace(d)
		if d != "" && !filepath.IsAbs(d) && !strings.HasPrefix(d, "~"+string(filepath.Separator)) {
			slog.Warn("daemon: auto_trust_dirs has a piece that is not an absolute or ~/ path; trusting no folder", "piece", d)
			return nil
		}
	}
	return dirs
}
