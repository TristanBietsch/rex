package boot

import (
	"log/slog"
	"os"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/features/usage"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
)

// UsageTap returns the server's Config.UsageTap: the statusLine command to
// inject into a Claude session's --settings, or "" when the usage feature or
// the usage_statusline_tap setting says not to (see usage.ShouldInjectTap).
// current is read at each spawn, so a change applies to the next session.
// socket is the daemon's own socket; it is put on the tap's command line
// only when it is not the default one.
func UsageTap(socket string, current func() *settings.Store) func(sessionID string) string {
	return func(sessionID string) string {
		st := current()
		home, _ := paths.Home()
		mode := st.String("usage_statusline_tap")
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
