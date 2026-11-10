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
// through symlinks. Relative entries, a filesystem root and the home folder
// itself are ignored: they would trust far more than one project.
func underAutoTrustDir(cwd string, dirs []string) (string, bool) {
	c, ok := canonicalDir(cwd)
	if !ok {
		return "", false
	}
	home := ""
	if h, err := paths.Home(); err == nil {
		home, _ = canonicalDir(h)
	}
	for _, entry := range dirs {
		d := strings.TrimSpace(entry)
		expanded, err := paths.ExpandHome(d)
		if d == "" || err != nil {
			continue
		}
		cd, ok := canonicalDir(expanded)
		if !ok || filepath.Dir(cd) == cd || cd == home {
			continue
		}
		if c == cd || strings.HasPrefix(c, cd+string(filepath.Separator)) {
			return d, true
		}
	}
	return "", false
}

// canonicalDir resolves an absolute path through symlinks in its deepest
// existing ancestor, so a folder not created yet still compares equal to
// its resolved parent (on macOS /var is a link to /private/var).
//
// A path with a ".." element is refused. The kernel applies ".." after
// following symlinks (<trusted>/link/../x lands next to link's target),
// while cleaning the path first would apply it before; refusing keeps the
// check from approving a folder the agent would not actually start in.
func canonicalDir(p string) (string, bool) {
	if !filepath.IsAbs(p) || hasDotDot(p) {
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

func hasDotDot(p string) bool {
	for _, part := range strings.Split(filepath.ToSlash(p), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}
