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
// there is none. It reads, never writes, these files and takes the first
// that sets a command-type statusLine, in Claude's precedence order:
//
//	<cwd>/.claude/settings.local.json
//	<cwd>/.claude/settings.json
//	$CLAUDE_CONFIG_DIR/settings.json (default ~/.claude/settings.json)
//
// Supported: statusLine of type "command" with a non-empty command. Not
// supported: enterprise managed settings, and a --settings file given by the
// user on their own claude command line (rex starts Claude itself). A file
// that is missing or is not valid JSON is skipped. A command that already
// runs the tap is ignored, so the tap never calls itself.
func UserStatusLine(home, cwd string) string {
	var files []string
	if cwd != "" {
		files = append(files,
			filepath.Join(cwd, ".claude", "settings.local.json"),
			filepath.Join(cwd, ".claude", "settings.json"))
	}
	cfg := os.Getenv("CLAUDE_CONFIG_DIR")
	if cfg == "" && home != "" {
		cfg = filepath.Join(home, ".claude")
	}
	if cfg != "" {
		files = append(files, filepath.Join(cfg, "settings.json"))
	}
	for _, f := range files {
		if cmd := statusLineCommand(f); cmd != "" {
			return cmd
		}
	}
	return ""
}

// statusLineCommand reads path and returns its statusLine command, "" when
// the file has none.
func statusLineCommand(path string) string {
	b, err := os.ReadFile(path)
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
