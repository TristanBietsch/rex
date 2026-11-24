package tui

import (
	"errors"
	"log/slog"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The trust choice of a new agent (P2): this session only trusts the
// session's folder for one session, always saves the repo folder to
// auto_trust_dirs. Nothing is trusted unless one is chosen, and the new-agent
// page always names the exact folder each choice trusts.

const trustSettingID = "auto_trust_dirs"

// wizTrustMode is what the user chose on the describe step.
type wizTrustMode int

const (
	trustAsk     wizTrustMode = iota // nothing chosen: the agent asks
	trustSession                     // ctrl+t: this session only
	trustAlways                      // ctrl+a: save the repo folder, then this session too
)

// trustOffer is what can be trusted for a session started in cwd.
type trustOffer struct {
	// Covered is the auto_trust_dirs entry that already covers cwd.
	Covered string
	// Refused says why cwd can never be trusted (empty when it can).
	Refused string
	// SessionDir is cwd, resolved: what ctrl+t trusts.
	SessionDir string
	// AlwaysEntry and AlwaysDir are the repo folder (D8) ctrl+a saves, as
	// the "~/..." entry and as the exact resolved path. Empty when there is
	// none (cwd is outside the home folder).
	AlwaysEntry, AlwaysDir string
}

func trustOfferFor(m Model, cwd string) trustOffer {
	var o trustOffer
	if m.Store != nil {
		if e, ok := settings.CoveredBy(cwd, m.Store.String(trustSettingID)); ok {
			o.Covered = e
			return o
		}
	}
	dir, err := settings.TrustDir(cwd)
	if err != nil {
		o.Refused = refusalText(cwd, err)
		return o
	}
	o.SessionDir = dir
	if e := settings.SuggestTrustDir(cwd); e != "" {
		if d, err := settings.TrustDir(e); err == nil {
			o.AlwaysEntry, o.AlwaysDir = e, d
		}
	}
	return o
}

// refusalText names why cwd cannot be trusted; the home folder, the common
// case when rex starts from ~, gets the pointer to a project folder.
func refusalText(cwd string, err error) string {
	if h, herr := settings.CanonicalHome(); herr == nil {
		if c, ok := settings.CanonicalDir(cwd); ok && c == h {
			return "home folder: start rex in a project folder to trust it"
		}
	}
	return err.Error()
}

// trustKeyToggle is ctrl+t / ctrl+a applied to the trust choice cur: the
// new choice, or cur with m.Err saying why it cannot change. ok is false
// for any other key. ctrl+t and ctrl+a use it on the new-agent page.
func trustKeyToggle(m Model, k tea.KeyMsg, cur wizTrustMode) (Model, wizTrustMode, bool) {
	var want wizTrustMode
	switch k.Type {
	case tea.KeyCtrlT:
		want = trustSession
	case tea.KeyCtrlA:
		want = trustAlways
	default:
		return m, cur, false
	}
	o := trustOfferFor(m, spawnCWD(m))
	switch {
	case o.Covered != "":
		return m, cur, true
	case o.Refused != "":
		m.Err = "trust: " + o.Refused
		return m, cur, true
	case want == trustAlways && o.AlwaysDir == "":
		m.Err = "trust: only a folder inside your home folder can be saved; ctrl+t trusts this session"
		return m, cur, true
	}
	m.Err = ""
	if cur == want {
		return m, trustAsk, true
	}
	return m, want, true
}

// trustRequest applies the trust choice mode to req just before launch.
// For "always" it saves the repo folder to auto_trust_dirs first; when that
// fails ok is false, m.Err says why and nothing is launched. A folder that
// is already covered needs nothing.
func trustRequest(m Model, mode wizTrustMode, cwd string, req protocol.NewSession) (Model, protocol.NewSession, bool) {
	if mode == trustAsk {
		return m, req, true
	}
	o := trustOfferFor(m, cwd)
	if o.Covered != "" {
		return m, req, true
	}
	if o.Refused != "" {
		m.Err = "trust: " + o.Refused
		return m, req, false
	}
	if mode == trustAlways {
		if m.Store == nil {
			m.Err = "trust: settings are not loaded; use ctrl+t or `rex trust`"
			return m, req, false
		}
		// The YAML layer's own list: a broken config.lua fails the
		// effective value closed (empty), and saving that plus the new
		// folder would drop every folder config.yaml lists.
		next, changed, err := settings.AppendTrustDir(m.Store.FileString(trustSettingID), o.AlwaysEntry)
		if err != nil {
			m.Err = "trust: " + err.Error()
			return m, req, false
		}
		if changed {
			if err := saveTrustDirs(&m, next); err != nil {
				m.Err = "trust: " + err.Error()
				return m, req, false
			}
			slog.Info("tui: trusted folder from the new-agent page", "dir", o.AlwaysDir)
		}
	}
	req.TrustFolder = true
	return m, req, true
}

// saveTrustDirs stores and saves a new auto_trust_dirs value, then tells the
// daemon to re-read it, as a Settings page edit does (with live_reload off
// the daemon would otherwise never see it).
func saveTrustDirs(m *Model, value string) error {
	if err := m.Store.Set(trustSettingID, value); err != nil {
		return err
	}
	if m.StorePath == "" {
		return errors.New("no config file path")
	}
	if err := m.Store.Save(m.StorePath); err != nil {
		return err
	}
	reloadDaemonForTrust(m.reloader, m.Socket, trustSettingID)
	return nil
}

// wrapPath cuts p into at most maxLines pieces of width cells so the exact
// path stays readable. Only a path longer than all of them loses its start.
func wrapPath(p string, width, maxLines int) []string {
	if w := ansi.StringWidth(p); w > width*maxLines {
		p = ansi.TruncateLeft(p, w-width*maxLines+1, "…")
	}
	var out []string
	for ansi.StringWidth(p) > width {
		out = append(out, ansi.Truncate(p, width, ""))
		p = ansi.TruncateLeft(p, width, "")
	}
	return append(out, p)
}

// typedPath cleans a folder typed or pasted into the command bar: escape
// sequences and other control characters a paste can carry are dropped, so
// they never reach config.yaml or the status line.
func typedPath(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, ansi.Strip(s))
}

// runTrustCmd is `:trust [path]`: save a folder to auto_trust_dirs. With no
// path it is the repo folder of the selected session's folder, or of the
// folder rex runs in. Sessions already running keep asking; new ones skip
// the dialog.
func runTrustCmd(m Model, c cmdCall) (Model, tea.Cmd) {
	if len(c.Args) > 1 {
		m.Err = "trust: usage :trust [folder]"
		return m, nil
	}
	if m.Store == nil {
		m.Err = "trust: settings are not loaded; use `rex trust`"
		return m, nil
	}
	entry := ""
	if len(c.Args) == 1 {
		if entry = typedPath(c.Args[0]); entry == "" {
			m.Err = "trust: usage :trust [folder]"
			return m, nil
		}
	} else {
		base := spawnCWD(m)
		for _, s := range m.Sessions {
			if s.ID == m.SelectedID && s.CWD != "" {
				base = s.CWD
			}
		}
		if entry = settings.SuggestTrustDir(base); entry == "" {
			m.Err = "trust: no project folder to trust for " + base + "; give one: :trust ~/dev/project"
			return m, nil
		}
	}
	dir, err := settings.TrustDir(entry)
	if err != nil {
		m.Err = "trust: " + err.Error()
		return m, nil
	}
	next, changed, err := settings.AppendTrustDir(m.Store.FileString(trustSettingID), entry) // YAML layer, see trustRequest
	if err != nil {
		m.Err = "trust: " + err.Error()
		return m, nil
	}
	m.Err = ""
	if !changed {
		m.Notice = "already trusted " + dir
		return m, nil
	}
	if err := saveTrustDirs(&m, next); err != nil {
		m.Err = "trust: " + err.Error()
		return m, nil
	}
	m.Notice = "trusted " + dir + " (new sessions skip the trust dialog)"
	return m, nil
}
