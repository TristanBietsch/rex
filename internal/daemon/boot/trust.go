package boot

import (
	"log/slog"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// SkipPermissions reads skip_permissions from config.yaml. The server calls
// it at each spawn, so a changed setting applies to the next session. On a
// read error it is off.
func SkipPermissions() bool {
	st, _, err := settings.LoadDefault()
	if err != nil {
		slog.Warn("daemon: settings load failed for skip_permissions; leaving it off", "err", err)
		return false
	}
	on, _ := st.Get("skip_permissions").(bool)
	return on
}

// ResumeEnabled reads the resume feature switch from config.yaml. The server
// calls it at each spawn and Resume. On a read error it uses the defaults
// (on).
func ResumeEnabled() bool {
	st, _, err := settings.LoadDefault()
	if err != nil {
		slog.Warn("daemon: settings load failed for resume; using the default", "err", err)
	}
	return settings.FeatureOn(st, "resume")
}

// AutoTrustDirs reads auto_trust_dirs from config.yaml. The server calls it
// at each spawn, so a changed setting applies to the next session without a
// daemon restart. On a read error it trusts nothing.
//
// A value settings.SplitTrustDirs refuses (a piece that is neither absolute
// nor ~/, as when a folder name holds the list separator) is ignored as a
// whole rather than guessed at.
func AutoTrustDirs() []string {
	st, _, err := settings.LoadDefault()
	if err != nil {
		slog.Warn("daemon: settings load failed for auto_trust_dirs; trusting no folder", "err", err)
		return nil
	}
	dirs, err := settings.SplitTrustDirs(st.String("auto_trust_dirs"))
	if err != nil {
		slog.Warn("daemon: auto_trust_dirs ignored; trusting no folder", "err", err)
		return nil
	}
	return dirs
}
