package tui

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// The trust line of the spawn wizard (P2): ctrl+t trusts the session's
// folder for this one session, ctrl+a saves the repo folder to
// auto_trust_dirs. Nothing is trusted unless one of them is pressed, and the
// line always names the exact folder each key trusts.

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

// wizardCWD is the folder a wizard spawn starts in: the TUI's own.
func wizardCWD() string {
	cwd, _ := os.Getwd()
	return cwd
}

// wizardTrustKey handles ctrl+t and ctrl+a on the describe step. ok is false
// for any other key. Each key toggles its mode and switches the other off.
func wizardTrustKey(m Model, k tea.KeyMsg) (Model, bool) {
	var want wizTrustMode
	switch k.Type {
	case tea.KeyCtrlT:
		want = trustSession
	case tea.KeyCtrlA:
		want = trustAlways
	default:
		return m, false
	}
	o := trustOfferFor(m, wizardCWD())
	switch {
	case o.Covered != "":
		return m, true
	case o.Refused != "":
		m.Err = "trust: " + o.Refused
		return m, true
	case want == trustAlways && o.AlwaysDir == "":
		m.Err = "trust: only a folder inside your home folder can be saved; ctrl+t trusts this session"
		return m, true
	}
	m.Err = ""
	if m.Wizard.Trust == want {
		m.Wizard.Trust = trustAsk
	} else {
		m.Wizard.Trust = want
	}
	return m, true
}

// wizardTrustRequest applies the chosen trust mode to req just before launch.
// For ctrl+a it saves the repo folder to auto_trust_dirs first; when that
// fails ok is false, m.Err says why and nothing is launched. A folder that
// is already covered needs nothing.
func wizardTrustRequest(m Model, cwd string, req protocol.NewSession) (Model, protocol.NewSession, bool) {
	if m.Wizard == nil || m.Wizard.Trust == trustAsk {
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
	if m.Wizard.Trust == trustAlways {
		if m.Store == nil {
			m.Err = "trust: settings are not loaded; use ctrl+t or `rex trust`"
			return m, req, false
		}
		next, changed, err := settings.AppendTrustDir(m.Store.String(trustSettingID), o.AlwaysEntry)
		if err != nil {
			m.Err = "trust: " + err.Error()
			return m, req, false
		}
		if changed {
			if err := saveTrustDirs(&m, next); err != nil {
				m.Err = "trust: " + err.Error()
				return m, req, false
			}
			slog.Info("tui: trusted folder from the wizard", "dir", o.AlwaysDir)
		}
	}
	req.TrustFolder = true
	return m, req, true
}

// saveTrustDirs stores and saves a new auto_trust_dirs value. The daemon
// picks it up from the file within a few seconds.
func saveTrustDirs(m *Model, value string) error {
	if err := m.Store.Set(trustSettingID, value); err != nil {
		return err
	}
	if m.StorePath == "" {
		return errors.New("no config file path")
	}
	return m.Store.Save(m.StorePath)
}

// wizTrustLines renders the trust block under the task input.
func wizTrustLines(m Model) []string {
	o := trustOfferFor(m, wizardCWD())
	switch {
	case o.Covered != "":
		return []string{"  " + styleDim.Render("trusted ("+trustSettingID+")")}
	case o.Refused != "":
		return []string{"  " + styleBootWarn.Render(o.Refused)}
	}
	lines := []string{"  " + styleBootWarn.Render("folder not trusted") + styleDim.Render(": the agent will ask")}
	lines = append(lines, trustOption(m.Wizard.Trust == trustSession, "ctrl+t", "trust this session only", o.SessionDir)...)
	if o.AlwaysDir == "" {
		return append(lines, "  "+styleDim.Render("ctrl+a: only folders inside your home can be saved"))
	}
	return append(lines, trustOption(m.Wizard.Trust == trustAlways, "ctrl+a", "always trust (saved to "+trustSettingID+")", o.AlwaysDir)...)
}

// trustOption is one choice: the key and what it does, then the exact folder
// on its own line (cut from the left when very long, so the project name
// stays).
func trustOption(on bool, key, what, dir string) []string {
	mark := "  "
	if on {
		mark = styleArrow.Render(selectGlyph()) + " "
	}
	out := []string{"  " + mark + styleKey.Render(key) + "  " + styleSlug.Render(what)}
	for _, part := range wrapPath(filepath.ToSlash(dir), wizardWidth-8, 2) {
		out = append(out, "        "+styleDim.Render(part))
	}
	return out
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
		entry = c.Args[0]
	} else {
		base := wizardCWD()
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
	next, changed, err := settings.AppendTrustDir(m.Store.String(trustSettingID), entry)
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
