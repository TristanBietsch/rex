package usage

import (
	"path/filepath"
	"runtime"
	"strings"
)

// Tap modes: the values of the usage_statusline_tap setting.
const (
	TapAuto = "auto"
	TapOn   = "on"
	TapOff  = "off"
)

// ShouldInjectTap decides whether a Claude session gets the status-line tap.
//
//   - usageOn is the usage feature switch; off means never.
//   - off never injects; on always does.
//   - auto injects only when the user already has a status line (see
//     UserStatusLine). A configured status line makes Claude hide its footer
//     hints ("esc to interrupt", "? for shortcuts"); rex must never cause
//     that on its own, so without a user status line auto leaves Claude
//     alone and the Claude rows use the estimate. Only the user-level status
//     line counts: a project-only one is neither passed through nor
//     replaced under auto.
//
// An unknown mode is treated as auto.
func ShouldInjectTap(usageOn bool, mode, home string) bool {
	if !usageOn {
		return false
	}
	switch mode {
	case TapOff:
		return false
	case TapOn:
		return true
	}
	return UserStatusLine(home) != ""
}

// TapCommand is the statusLine.command rex injects: the rex-daemon binary at
// exe, run as `statusline-tap --session <id>`, plus --socket when the daemon
// listens somewhere other than the default. Arguments are shell-quoted,
// because Claude runs the command through a shell.
func TapCommand(exe, sessionID, socket string) string {
	parts := []string{shellQuote(exe), TapSubcommand, "--session", shellQuote(sessionID)}
	if socket != "" {
		parts = append(parts, "--socket", shellQuote(socket))
	}
	return strings.Join(parts, " ")
}

// shellQuote quotes s for a POSIX shell (Git Bash on Windows too, where
// backslashes in a path become slashes first).
func shellQuote(s string) string {
	if runtime.GOOS == "windows" {
		s = filepath.ToSlash(s)
	}
	safe := s != ""
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && !strings.ContainsRune("_-./:@%+=", r) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
