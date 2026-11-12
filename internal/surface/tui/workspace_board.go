package tui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/catalog/theme"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// WorkspaceScope is the fixed workspace a board is opened on (`rex here`,
// `rex ws open`). The fields come from the workspace's own .rex/ on this
// machine, so Name is already cleaned and Color is "#RRGGBB" or "".
type WorkspaceScope struct {
	ID    string
	Name  string
	Color string
	Root  string
}

// wsOldDaemonNotice is shown once when the daemon does not tag sessions.
const wsOldDaemonNotice = "daemon doesn't tag workspaces — `rex daemon restart`"

// wsScope is the workspace this board is scoped to: nil on an ordinary
// board, and while the workspaces feature is off (the scope is inert then,
// whatever the board was opened with).
func (m Model) wsScope() *WorkspaceScope {
	if m.WS == nil || !m.featureOn("workspaces") {
		return nil
	}
	return m.WS
}

// wsFilterID is the workspace id the row filter applies: "" on an ordinary
// board, and on a scoped board whose daemon does not tag sessions (it shows
// every session, with one notice).
func (m Model) wsFilterID() string {
	if sc := m.wsScope(); sc != nil && m.wsTagging {
		return sc.ID
	}
	return ""
}

// scopedSessions is m.Sessions narrowed to the board's workspace: what the
// header counts, the screensaver, the overview line and :prune work on.
func scopedSessions(m Model) []protocol.SessionSummary {
	id := m.wsFilterID()
	if id == "" {
		return m.Sessions
	}
	out := make([]protocol.SessionSummary, 0, len(m.Sessions))
	for _, s := range m.Sessions {
		if s.WorkspaceID == id {
			out = append(out, s)
		}
	}
	return out
}

// adoptWorkspaceTagging reads what a snapshot says about workspace tagging.
// A daemon tags when it says so, or when any session already carries an id;
// otherwise a scoped board shows every session and says why, once.
func adoptWorkspaceTagging(m Model, snap *protocol.Snapshot) Model {
	m.wsTagging = snap.WorkspaceTagging
	for _, s := range snap.Sessions {
		if s.WorkspaceID != "" {
			m.wsTagging = true
			break
		}
	}
	if m.wsScope() != nil && !m.wsTagging && !m.wsNoticed {
		m.wsNoticed = true
		m.Notice = wsOldDaemonNotice
	}
	return reselectOutOfScope(m)
}

// reselectOutOfScope moves a selection that the workspace scope hides to the
// first row the board shows.
func reselectOutOfScope(m Model) Model {
	if m.wsFilterID() == "" || m.SelectedID == "" || sessionIndex(scopedSessions(m), m.SelectedID) >= 0 {
		return m
	}
	m.SelectedID = ""
	if rows := orderedSessions(m); len(rows) > 0 {
		m.SelectedID = rows[0].ID
	}
	return m
}

// noteSessionWorkspace flips a board whose daemon turned tagging on after
// the snapshot (the setting changed, the daemon restarted): a tagged session
// is proof it tags.
func noteSessionWorkspace(m Model, sum protocol.SessionSummary) Model {
	if sum.WorkspaceID != "" && !m.wsTagging {
		m.wsTagging = true
	}
	return m
}

// spawnCWD is where a new session starts: the workspace root on a workspace
// board, else the folder the TUI was started in.
func spawnCWD(m Model) string {
	if sc := m.wsScope(); sc != nil && sc.Root != "" {
		return sc.Root
	}
	cwd, _ := os.Getwd()
	return cwd
}

// wsEdge is the workspace's color as an edge color, ok false when it has
// none (a color that is not "#RRGGBB" never reaches the terminal).
func wsEdge(sc *WorkspaceScope) (theme.EdgeColor, bool) {
	if sc == nil || sc.Color == "" {
		return theme.EdgeColor{}, false
	}
	c, err := theme.ParseEdge(sc.Color)
	if err != nil {
		return theme.EdgeColor{}, false
	}
	return c, true
}

// edgeTerminalColor is the terminal color of c under m's theme: the ansi
// scheme (no background of its own) uses the palette index when there is one.
func edgeTerminalColor(m Model, c theme.EdgeColor) lipgloss.Color {
	p := borderPalette(m)
	if p.Bg == "" && c.ANSI != "" {
		return lipgloss.Color(c.ANSI)
	}
	return lipgloss.Color(c.Hex(p.Dark))
}

// maxChipNameCells caps the workspace name in the header chip.
const maxChipNameCells = 24

// renderWorkspaceChip is "[<id> <name>]" in the workspace color, the first
// thing in the header of a workspace board; "" otherwise. A narrow layout
// drops the name. Without color (NO_COLOR) the text carries it all.
func renderWorkspaceChip(m Model) string {
	sc := m.wsScope()
	if sc == nil {
		return ""
	}
	text := "[" + sc.ID
	if name := protocol.FlattenDisplayText(sc.Name); name != "" && !mobileLayout(m) {
		text += " " + ansi.Truncate(name, maxChipNameCells, "…")
	}
	text += "]"
	st := lipgloss.NewStyle().Bold(true)
	if c, ok := wsEdge(sc); ok {
		st = st.Foreground(edgeTerminalColor(m, c))
	}
	return st.Render(text)
}

// workspaceBorderDefault is whether a workspace board draws the window
// border even though window_border was never set: it does, so the workspace
// color is visible. An explicit window_border (in config or Settings) wins,
// and so does :border.
func workspaceBorderDefault(m Model) bool {
	if m.wsScope() == nil || m.Store == nil {
		return false
	}
	return m.Store.Source("window_border") == settings.SourceDefault
}
