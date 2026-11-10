package server

import (
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/daemon/pty"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
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
// which entry matched. Entries may start with ~/; both sides are resolved
// through symlinks. Relative entries and a filesystem root are ignored: a
// root would trust every folder.
func underAutoTrustDir(cwd string, dirs []string) (string, bool) {
	c, ok := canonicalDir(cwd)
	if !ok {
		return "", false
	}
	for _, entry := range dirs {
		d := strings.TrimSpace(entry)
		if d == "~" {
			d = "~" + string(filepath.Separator)
		}
		expanded, err := paths.ExpandHome(d)
		if d == "" || err != nil {
			continue
		}
		cd, ok := canonicalDir(expanded)
		if !ok || filepath.Dir(cd) == cd {
			continue
		}
		if c == cd || strings.HasPrefix(c, cd+string(filepath.Separator)) {
			return d, true
		}
	}
	return "", false
}

// canonicalDir cleans an absolute path and resolves symlinks in its deepest
// existing ancestor, so a folder not created yet still compares equal to
// its resolved parent (on macOS /var is a link to /private/var).
func canonicalDir(p string) (string, bool) {
	if !filepath.IsAbs(p) {
		return "", false
	}
	p = filepath.Clean(p)
	rest := ""
	for cur := p; ; {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(r, rest), true
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p, true
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}
