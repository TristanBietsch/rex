package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
)

// naGoldenScreens is every new-agent golden screen.
func naGoldenScreens() []goldenScreen {
	var out []goldenScreen
	for _, sc := range goldenScreens() {
		if strings.HasPrefix(sc.name, "newagent_") {
			out = append(out, sc)
		}
	}
	return out
}

// naHasCursor reports whether a plain screen shows where focus is: the ▸
// row cursor or the text cursor; a preview shows the item it is on.
func naHasCursor(name, plain string) bool {
	if strings.Contains(name, "preview") {
		return strings.Contains(plain, "PASTE 1")
	}
	return strings.Contains(plain, "▸") || strings.Contains(plain, "█")
}

// Each screen, resized from 120×40 to 50×20 to 80×24 through the real
// WindowSizeMsg path, is exactly h lines, no line wider than w, and still
// shows its cursor.
func TestNewAgent_ResizeEveryScreen(t *testing.T) {
	for _, sc := range naGoldenScreens() {
		if sc.name == "newagent_frame" {
			continue // the frame is pinned to its terminal size
		}
		t.Run(sc.name, func(t *testing.T) {
			m := sc.model(t, goldenSize{120, 40})
			for _, sz := range []goldenSize{{120, 40}, {50, 20}, {80, 24}} {
				next, _ := m.Update(tea.WindowSizeMsg{Width: sz.w, Height: sz.h})
				m = next.(Model)
				view := m.View()
				lines := strings.Split(view, "\n")
				require.Len(t, lines, sz.h, "%dx%d", sz.w, sz.h)
				for i, l := range lines {
					require.LessOrEqual(t, ansi.StringWidth(l), sz.w, "%dx%d line %d: %q", sz.w, sz.h, i, ansi.Strip(l))
				}
				require.True(t, naHasCursor(sc.name, ansi.Strip(view)), "%dx%d: no cursor\n%s", sz.w, sz.h, ansi.Strip(view))
			}
		})
	}
}

// No state is told by color alone: with every escape stripped, each golden
// screen still says it in a glyph or a word.
func TestNewAgent_NoColorOnlyState(t *testing.T) {
	for _, sc := range naGoldenScreens() {
		for _, sz := range sc.sizesOf() {
			m := sc.model(t, sz)
			sizeGolden(&m, sz)
			plain := ansi.Strip(m.View())
			at := fmt.Sprintf("%s %dx%d", sc.name, sz.w, sz.h)
			for _, l := range strings.Split(plain, "\n") {
				body := strings.TrimSpace(strings.Trim(l, "│ "))
				if strings.Contains(body, "launch failed") || strings.Contains(body, "is outside") {
					require.True(t, strings.HasPrefix(body, "✗"), "%s: error without ✗: %q", at, body)
				}
				if strings.Contains(body, "secrets") && strings.Contains(body, ".pem") {
					require.Contains(t, body, "!", "%s: warning without !: %q", at, body)
				}
			}
			st := m.NewAgent
			require.NotNil(t, st, at)
			switch st.Screen {
			case naAgentScreen:
				if !st.Filtering && st.Pane == naPaneAgents {
					require.Contains(t, plain, "▸", "%s: the selected agent has ▸", at)
				}
				if len(st.selection().effortOptions()) > 1 {
					require.Contains(t, plain, "[", "%s: the effort is bracketed", at)
				}
			case naCompose:
				trust := naTrustOf(m, st).word
				if pageNarrow(sz.w) {
					require.Contains(t, plain, "trust: "+trust, "%s: the trust word", at)
				} else {
					require.Regexp(t, `Trust +`+trust, plain, "%s: the rail's trust word", at)
				}
			case naPreview, naImportScreen: // their marks are checked line by line above
			}
		}
	}
}

// naSpecKeys is every key of the spec's Keys table (R16 "Keys").
var naSpecKeys = []string{
	"n", ":new", "tab", "shift+tab", "1-5", "ctrl+g", "enter", "ctrl+s", "ctrl+j", "alt+enter",
	"paste", "@", "ctrl+o", "ctrl+e", "ctrl+u", "ctrl+w", "alt+backspace", "←", "→", "home", "end",
	"↑", "↓", "j", "k", "x", "delete", "r", "ctrl+t", "ctrl+a", "/", "h", "l", "space", "esc", "?",
}

// ? on the page (not in the editor) opens help on the new-agent group
// alone, every key of the spec's table in it; esc goes back to the page.
func TestNewAgent_HelpGroup(t *testing.T) {
	var keys []string
	for _, b := range bindings {
		if b.Ctx&ctxNewAgent != 0 {
			require.Equal(t, groupNewAgent, b.Group, b.Keys)
			keys = append(keys, strings.FieldsFunc(b.Keys, func(r rune) bool { return r == ' ' || r == ',' })...)
		}
	}
	for _, k := range naSpecKeys {
		require.Contains(t, keys, k, "the new-agent help group lists %q", k)
	}

	m := naOpen(t, naModel(t))
	m, _ = naKey(m, key("?"))
	require.Equal(t, FocusNewAgent, m.Focus, "in the editor ? is a letter")
	require.Equal(t, "?", m.NewAgent.Task.String())

	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyTab}) // Context
	m, _ = naKey(m, key("?"))
	require.Equal(t, FocusHelp, m.Focus)
	require.NotNil(t, m.Help)
	require.Equal(t, groupNewAgent, m.Help.Only)
	for _, pages := range []bool{true, false} {
		m = setFlag(t, m, "fullscreen_pages", pages)
		groups := helpGroups(m)
		require.Len(t, groups, 1)
		require.Equal(t, groupNewAgent, groups[0].Title)
		out := ansi.Strip(m.View())
		require.Contains(t, out, "ctrl+g")
		require.Contains(t, out, "import picker")
		require.NotContains(t, out, "mark done / undo", "board rows are filtered out")
	}

	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	require.Equal(t, FocusNewAgent, m.Focus, "esc goes back to the page")
	require.NotNil(t, m.NewAgent)
	require.Equal(t, "?", m.NewAgent.Task.String(), "the draft is untouched")

	m = openHelp(closeNewAgent(m, true))
	require.Empty(t, m.Help.Only, "help from the board shows every group")
	m = closeHelp(m)
	require.Equal(t, FocusBoard, m.Focus)
}

// The Agent screen's ? also opens the group, unless the filter takes keys.
func TestNewAgent_HelpFromAgentScreen(t *testing.T) {
	m := naOpen(t, naModel(t))
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlG})
	m, _ = naKey(m, key("?"))
	require.Equal(t, FocusHelp, m.Focus)
	m, _ = naKey(m, key("?"))
	require.Equal(t, FocusNewAgent, m.Focus)
	require.Equal(t, naAgentScreen, m.NewAgent.Screen)

	m, _ = naKey(m, key("/"))
	m, _ = naKey(m, key("?"))
	require.Equal(t, FocusNewAgent, m.Focus, "the filter takes ?")
	require.Equal(t, "?", m.NewAgent.Filter)
}

// Mockup G: at 50 columns the header keeps "draft" (the folder goes), the
// agent line drops the word effort, and the strip counts the context.
func TestNewAgent_NarrowMockupG(t *testing.T) {
	m := naOpen(t, naModel(t))
	m.Width, m.Height = 50, 20
	m = naType(m, "fix the flaky auth test")
	require.NoError(t, addPaste(m.NewAgent, strings.Repeat("log line\n", 10)))
	require.NoError(t, addPaste(m.NewAgent, strings.Repeat("more\n", 10)))
	out := ansi.Strip(m.View())
	lines := strings.Split(out, "\n")
	require.Contains(t, lines[1], "∴ REX › NEW AGENT")
	require.True(t, strings.HasSuffix(strings.TrimRight(lines[1], " "), "draft"), "%q", lines[1])
	require.NotContains(t, lines[1], "~/dev/proj")
	require.Contains(t, out, "‹ Agent · [Task] · Context 2 · Trust · Launch ›")
	require.Contains(t, out, "◆ Claude Code · opus · max")
	require.NotContains(t, out, "effort")
	require.Contains(t, out, "2 context items")

	m.Width = 80
	wide := strings.Split(ansi.Strip(m.View()), "\n")
	require.Contains(t, wide[1], "~/dev/proj", "wide keeps the folder")
	require.Contains(t, ansi.Strip(m.View()), "effort max")
}

// Mockup F: until the daemon's tool list is known, the AGENT section says
// it is checking; an old daemon (a snapshot without one) keeps today's
// every-tool-counts fallback without the row.
func TestNewAgent_AgentLoadingRow(t *testing.T) {
	m := naModel(t)
	m.DaemonTools, m.toolsKnown = nil, false
	m = naOpen(t, m)
	m.Width, m.Height = 80, 24
	require.Contains(t, ansi.Strip(m.View()), "checking which agents are installed…")
	m.Width, m.Height = 50, 20
	require.Contains(t, ansi.Strip(m.View()), "checking which agents")

	m = adoptSnapshotTools(m, nil) // an old daemon: no list in the snapshot
	m.Width, m.Height = 80, 24
	out := ansi.Strip(m.View())
	require.NotContains(t, out, "checking which agents")
	require.Contains(t, out, "Claude Code")

	m.toolsSnap = false
	m = adoptDaemonTools(m, daemonTools(t, "claude", "codex"))
	require.NotContains(t, ansi.Strip(m.View()), "checking which agents")
}

// The Agent screen's footer offers h/l only when the model has an effort.
func TestNewAgent_EffortHintOnlyWithEffort(t *testing.T) {
	st := &NewAgentState{Tools: []registry.Tool{{ID: "x", Name: "X"}}}
	hints := naAgentHints(st, false)
	for _, h := range hints {
		require.NotEqual(t, "h/l", h[0], "no effort options, no h/l")
	}
}
