package server

import (
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/daemon/pty"
)

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
