package server

import (
	"encoding/json"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/pty"
)

// trustDecision is the folder-trust handling chosen for one spawn: argv the
// agent trusts the folder with, and/or an answer for its trust dialog. The
// zero value means the user answers the dialog.
type trustDecision struct {
	Args   []string
	Answer *pty.TrustAnswer
}

// trustDecisionFor computes both halves of the trust decision for tool in cwd.
func (s *Server) trustDecisionFor(tool registry.Tool, cwd string) trustDecision {
	return trustDecision{Args: s.trustArgsFor(tool, cwd), Answer: s.trustAnswerFor(tool, cwd)}
}

// trustAnswerFor returns what accepts tool's folder-trust dialog when cwd is
// under one of the user's auto_trust_dirs, else nil (the user answers it).
func (s *Server) trustAnswerFor(tool registry.Tool, cwd string) *pty.TrustAnswer {
	tp := tool.TrustPrompt
	if tp == nil || s.cfg.AutoTrustDirs == nil {
		return nil
	}
	dir, ok := underAutoTrustDir(cwd, s.cfg.AutoTrustDirs())
	if !ok {
		return nil
	}
	shown, err := regexp.Compile(tp.Shown)
	if err != nil {
		slog.Warn("spawn: trust_prompt.shown does not compile", "tool", tool.ID, "err", err)
		return nil
	}
	selected, err := regexp.Compile(tp.Selected)
	if err != nil {
		slog.Warn("spawn: trust_prompt.selected does not compile", "tool", tool.ID, "err", err)
		return nil
	}
	slog.Info("spawn: auto-trust enabled", "tool", tool.ID, "auto_trust_dir", dir)
	return &pty.TrustAnswer{Shown: shown, Selected: selected, Move: []byte(tp.Move), Confirm: []byte(tp.Confirm)}
}

// trustArgsFor returns tool.TrustArgs with {cwd_list} expanded to a JSON
// array of the session folder (as given and, when different, resolved
// through symlinks) when cwd is under one of the user's auto_trust_dirs,
// else nil. The agent then trusts the folder itself and shows no dialog.
func (s *Server) trustArgsFor(tool registry.Tool, cwd string) []string {
	if len(tool.TrustArgs) == 0 || s.cfg.AutoTrustDirs == nil {
		return nil
	}
	dir, ok := underAutoTrustDir(cwd, s.cfg.AutoTrustDirs())
	if !ok {
		return nil
	}
	folders := []string{filepath.Clean(cwd)}
	if c, ok := settings.CanonicalDir(cwd); ok && c != folders[0] {
		folders = append(folders, c)
	}
	list, err := json.Marshal(folders)
	if err != nil {
		return nil
	}
	out := make([]string, len(tool.TrustArgs))
	for i, a := range tool.TrustArgs {
		out[i] = strings.ReplaceAll(a, registry.TrustArgsCWDList, string(list))
	}
	slog.Info("spawn: auto-trust via args", "tool", tool.ID, "auto_trust_dir", dir)
	return out
}

// underAutoTrustDir reports whether cwd is one of dirs or inside one, and
// which entry matched. Both sides are resolved through symlinks. Entries
// that settings.TrustDir refuses (relative, "..", a filesystem root, the
// home folder itself) are skipped: they would trust far more than one
// project.
func underAutoTrustDir(cwd string, dirs []string) (string, bool) {
	c, ok := settings.CanonicalDir(cwd)
	if !ok {
		return "", false
	}
	for _, entry := range dirs {
		cd, err := settings.TrustDir(entry)
		if err != nil {
			continue
		}
		if c == cd || strings.HasPrefix(c, cd+string(filepath.Separator)) {
			return strings.TrimSpace(entry), true
		}
	}
	return "", false
}
