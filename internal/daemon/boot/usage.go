package boot

import (
	"log/slog"
	"os"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/features/usage"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
)

// UsageEnabled reads the usage feature switch from config.yaml. On a read
// error it uses the defaults (on).
func UsageEnabled() bool {
	st, _, err := settings.LoadDefault()
	if err != nil {
		slog.Warn("daemon: settings load failed for usage; using the default", "err", err)
	}
	return settings.FeatureOn(st, "usage")
}

// UsageTap returns the server's Config.UsageTap: the statusLine command to
// inject into a Claude session's --settings, or "" when the usage feature or
// the usage_statusline_tap setting says not to (see usage.ShouldInjectTap).
// Settings are read at each spawn, so a change applies to the next session.
// socket is the daemon's own socket; it is put on the tap's command line
// only when it is not the default one.
func UsageTap(socket string) func(sessionID string) string {
	return func(sessionID string) string {
		st, _, err := settings.LoadDefault()
		if err != nil {
			slog.Warn("daemon: settings load failed for the usage tap; using the defaults", "err", err)
		}
		home, _ := paths.Home()
		mode := usage.TapAuto
		if st != nil {
			mode = st.String("usage_statusline_tap")
		}
		// Resolved here, from the daemon's own environment, and baked into
		// the command line: the tap never looks at its own.
		userSettings := usage.UserSettingsPath(home)
		if !usage.ShouldInjectTap(settings.FeatureOn(st, "usage"), mode, userSettings) {
			return ""
		}
		exe, err := os.Executable()
		if err != nil {
			slog.Warn("daemon: usage tap not injected: cannot find the rex-daemon binary", "err", err)
			return ""
		}
		if def, err := paths.Socket(); err == nil && def == socket {
			socket = ""
		}
		return usage.TapCommand(exe, sessionID, socket, userSettings)
	}
}
