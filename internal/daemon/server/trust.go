package server

import (
	"encoding/json"
	"fmt"
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
	// Trusted is whether the folder is one the user trusts: under
	// auto_trust_dirs, or trusted for this session. Only a trusted folder
	// gets skip_permissions_args.
	Trusted bool
}

// Why a folder is trusted, as logged.
const (
	trustWhyListed  = "auto_trust_dirs"
	trustWhySession = "session"
)

// trustDecisionFor computes both halves of the trust decision for tool in
// cwd. session is the session's own trust request (NewSession.TrustFolder,
// kept for Resume): it trusts cwd as if it were listed, when cwd passes
// sessionTrustDir.
func (s *Server) trustDecisionFor(tool registry.Tool, cwd string, session bool) trustDecision {
	dir, why := s.trustedDir(cwd, session)
	if why == "" {
		return trustDecision{}
	}
	return trustDecision{
		Args:    s.trustArgsFor(tool, cwd, dir, why),
		Answer:  s.trustAnswerFor(tool, dir, why),
		Trusted: true,
	}
}

// trustedDir returns the folder that makes cwd trusted and why: "" when it is
// not trusted. A listed folder wins over the session's own request.
func (s *Server) trustedDir(cwd string, session bool) (dir, why string) {
	if d, ok := underAutoTrustDirs(s, cwd); ok {
		return d, trustWhyListed
	}
	if !session {
		return "", ""
	}
	d, err := sessionTrustDir(cwd)
	if err != nil {
		slog.Warn("spawn: trust_folder ignored", "cwd", cwd, "err", err)
		return "", ""
	}
	return d, trustWhySession
}

// sessionTrustDir resolves cwd for a per-session trust request. It applies
// the checks of an auto_trust_dirs entry (settings.TrustDir: no "..", not a
// filesystem root, not the home folder) and also needs an absolute path,
// which a spawn folder always is but a "~/" entry would not be.
func sessionTrustDir(cwd string) (string, error) {
	if !filepath.IsAbs(cwd) {
		return "", fmt.Errorf("%q is not an absolute path", cwd)
	}
	return settings.TrustDir(cwd)
}

// underAutoTrustDirs reports whether cwd is under one of the configured
// auto_trust_dirs.
func underAutoTrustDirs(s *Server, cwd string) (string, bool) {
	if s.cfg.AutoTrustDirs == nil {
		return "", false
	}
	return underAutoTrustDir(cwd, s.cfg.AutoTrustDirs())
}

// trustAnswerFor returns what accepts tool's folder-trust dialog in the
// trusted folder dir (see trustedDir), else nil (the user answers it).
func (s *Server) trustAnswerFor(tool registry.Tool, dir, why string) *pty.TrustAnswer {
	tp := tool.TrustPrompt
	if tp == nil || why == "" {
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
	slog.Info("spawn: auto-trust enabled", "tool", tool.ID, "auto_trust_dir", dir, "why", why)
	return &pty.TrustAnswer{Shown: shown, Selected: selected, Move: []byte(tp.Move), Confirm: []byte(tp.Confirm)}
}

// trustArgsFor returns tool.TrustArgs with {cwd_list} expanded to a JSON
// array of the session folder (as given and, when different, resolved
// through symlinks) when cwd is trusted (dir and why from trustedDir), else
// nil. The agent then trusts the folder itself and shows no dialog.
func (s *Server) trustArgsFor(tool registry.Tool, cwd, dir, why string) []string {
	if len(tool.TrustArgs) == 0 || why == "" {
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
	slog.Info("spawn: auto-trust via args", "tool", tool.ID, "auto_trust_dir", dir, "why", why)
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
