package usage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// TapSubcommand is the rex-daemon subcommand that runs the tap.
const TapSubcommand = "statusline-tap"

// UserStatusLine returns the user's own Claude status-line command, "" when
// there is none. It reads only the user-level settings file, read-only:
//
//	$CLAUDE_CONFIG_DIR/settings.json (default ~/.claude/settings.json)
//
// and takes its statusLine when that is of type "command" with a non-empty
// command. Project-level files (<folder>/.claude/settings.json and
// settings.local.json) are deliberately never read. The tap runs this command
// on every status-line render, and an agent can write a project settings file
// from inside its own folder at any time; Claude itself reads those files
// only at startup and only for a trusted folder, so honoring them here would
// let an agent run a command of its choosing outside Claude's permission
// system. Not supported either: enterprise managed settings, and a
// --settings file the user passes to claude themselves (rex starts Claude).
// A missing or invalid file means no status line. A command that already
// runs the tap is ignored, so the tap never calls itself.
func UserStatusLine(home string) string {
	cfg := os.Getenv("CLAUDE_CONFIG_DIR")
	if cfg == "" {
		if home == "" {
			return ""
		}
		cfg = filepath.Join(home, ".claude")
	}
	return statusLineCommand(filepath.Join(cfg, "settings.json"))
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
