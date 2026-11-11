package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// TapSubcommand is the rex-daemon subcommand that runs the tap.
const TapSubcommand = "statusline-tap"

// UserSettingsPath is the absolute path of the user-level Claude settings
// file, resolved from the *daemon's* environment: $CLAUDE_CONFIG_DIR/settings.json
// when that is set to an absolute directory, else <home>/.claude/settings.json.
// "" when neither is absolute.
//
// The daemon resolves it once per spawn and bakes it into the tap's command
// line (--user-settings). The tap must not resolve it itself: Claude applies
// the env block of settings files, project ones included, to the processes
// it starts, so an agent that writes a project settings file could point a
// tap's own CLAUDE_CONFIG_DIR or HOME at a file it controls.
func UserSettingsPath(home string) string {
	if cfg := os.Getenv("CLAUDE_CONFIG_DIR"); cfg != "" {
		if filepath.IsAbs(cfg) {
			return filepath.Join(cfg, "settings.json")
		}
		return ""
	}
	if home == "" || !filepath.IsAbs(home) {
		return ""
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// UserStatusLine returns the user's own Claude status-line command from the
// settings file at path, "" when there is none. path must be absolute (as
// UserSettingsPath gives) and name a regular file: a relative path, a
// symlink or anything else that is not a regular file yields "", so a link
// an agent plants cannot redirect the read. Only that one file is read, and
// read-only. Project-level files (<folder>/.claude/settings*.json) are never
// read: an agent can write them at any time, and Claude itself reads them
// only at startup for a trusted folder, so honoring them would let an agent
// run a command of its choosing outside Claude's permission system. Not
// supported either: enterprise managed settings, and a --settings file the
// user passes to claude themselves (rex starts Claude).
//
// A statusLine of type "command" with a non-empty command that does not
// itself run the tap is returned.
func UserStatusLine(path string) string {
	if path == "" || !filepath.IsAbs(path) {
		return ""
	}
	if fi, err := os.Lstat(path); err != nil || !fi.Mode().IsRegular() {
		return ""
	}
	return statusLineCommand(path)
}

// statusLineCommand reads path and returns its statusLine command, "" when
// the file has none.
func statusLineCommand(path string) string {
	b, err := os.ReadFile(path) //nolint:gosec // G703: the user's own Claude settings file ($CLAUDE_CONFIG_DIR or ~/.claude), read-only.
	if err != nil {
		return ""
	}
	var doc struct {
		StatusLine *struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"statusLine"`
	}
	if json.Unmarshal(b, &doc) != nil || doc.StatusLine == nil {
		return ""
	}
	cmd := strings.TrimSpace(doc.StatusLine.Command)
	if doc.StatusLine.Type != "command" || cmd == "" || strings.Contains(cmd, TapSubcommand) {
		return ""
	}
	return cmd
}
