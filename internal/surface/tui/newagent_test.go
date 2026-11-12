package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/settings"
)

// naModel is a board with settings loaded, HOME isolated, the folder rex
// runs in a checkout inside it, and claude and codex installed.
func naModel(t *testing.T) Model {
	t.Helper()
	home := t.TempDir()
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	proj := filepath.Join(home, "dev", "proj")
	require.NoError(t, os.MkdirAll(filepath.Join(proj, ".git"), 0o700))
	t.Chdir(proj)
	m := boardModel()
	m.Width, m.Height = 120, 40
	m.Store = settings.NewStore()
	m.StorePath = filepath.Join(home, "config.yaml")
	m.reloader = func(context.Context, string) error { return nil } // never a real daemon
	m.DaemonTools, m.toolsKnown = daemonTools(t, "claude", "codex"), true
	return m
}

// naKey sends k through updateKey and returns the command too.
func naKey(m Model, k tea.KeyMsg) (Model, tea.Cmd) { return updateKey(m, k) }

func naOpen(t *testing.T, m Model) Model {
	t.Helper()
	m, _ = naKey(m, key("n"))
	require.Equal(t, FocusNewAgent, m.Focus)
	require.NotNil(t, m.NewAgent)
	return m
}

func naType(m Model, s string) Model {
	for _, r := range s {
		m, _ = naKey(m, key(string(r)))
	}
	return m
}

func setFlag(t *testing.T, m Model, id string, v any) Model {
	t.Helper()
	require.NoError(t, m.Store.Set(id, v))
	return m
}

func TestNewAgent_FlagRoutesN(t *testing.T) {
	m := naModel(t)
	on, _ := naKey(m, key("n"))
	require.Equal(t, FocusNewAgent, on.Focus)
	require.NotNil(t, on.NewAgent)
	require.Nil(t, on.Wizard)
	require.Equal(t, naTask, on.NewAgent.Section, "the page lands in the task editor")

	cmdOn, _ := executeCommand(m, "new")
	require.Equal(t, FocusNewAgent, cmdOn.Focus)

	m = setFlag(t, m, "new_agent_page", false)
	off, _ := naKey(m, key("n"))
	require.Equal(t, FocusWizard, off.Focus)
	require.Nil(t, off.NewAgent)
	cmdOff, _ := executeCommand(m, "new")
	require.Equal(t, FocusWizard, cmdOff.Focus)
}

func TestNewAgent_PasteNeverLaunches(t *testing.T) {
	m := naOpen(t, naModel(t))
	m, cmd := naKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fix\nit\n"), Paste: true})
	require.Nil(t, cmd, "a paste never launches")
	require.Equal(t, FocusNewAgent, m.Focus)
	require.Equal(t, "fix\nit\n", m.NewAgent.Task.String())
	require.Empty(t, m.PendingSelectSlug)
}

func TestNewAgent_EnterLaunchesCtrlJNewline(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naType(m, "fix")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = naType(m, "it")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	m = naType(m, "now")
	require.Equal(t, "fix\nit\nnow", m.NewAgent.Task.String())
	require.Equal(t, FocusNewAgent, m.Focus, "newlines never launch")

	st := m.NewAgent
	req, err := st.request(spawnCWD(m), existingSlugs(m))
	require.NoError(t, err)
	require.Equal(t, "fix\nit\nnow", req.InitialPrompt)
	require.Equal(t, "fix", req.Title, "the title is the first line")
	want := quickChoice{Tool: req.ToolID, Model: req.ModelID, Effort: req.Effort}

	m, cmd := naKey(m, key("enter"))
	require.NotNil(t, cmd, "enter in the task editor launches")
	require.Equal(t, FocusBoard, m.Focus)
	require.Nil(t, m.NewAgent)
	require.Nil(t, m.newAgentDraft, "a launch clears the draft")
	require.Equal(t, req.Slug, m.PendingSelectSlug)
	require.Equal(t, &want, m.lastSpawn, "the launch is remembered (D3)")
}

func TestNewAgent_EnterOnlyLaunchesFromTheEditor(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naType(m, "task")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyTab}) // Context
	require.Equal(t, naContext, m.NewAgent.Section)
	m, cmd := naKey(m, key("enter"))
	require.Nil(t, cmd)
	require.Equal(t, FocusNewAgent, m.Focus)

	// Typing outside the editor does not edit the task.
	m = naType(m, "zz")
	require.Equal(t, "task", m.NewAgent.Task.String())

	m, cmd = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	require.NotNil(t, cmd, "ctrl+s launches from any section")
	require.Equal(t, FocusBoard, m.Focus)
}

func TestNewAgent_EmptyTaskLaunchesLikeTheWizard(t *testing.T) {
	m := naOpen(t, naModel(t))
	m, cmd := naKey(m, key("enter"))
	require.NotNil(t, cmd)
	require.Equal(t, FocusBoard, m.Focus)
	require.NotEmpty(t, m.PendingSelectSlug, "an empty task gets a hash slug, as in the wizard")
}

func TestNewAgent_TabCyclesSections(t *testing.T) {
	m := naOpen(t, naModel(t))
	tab := tea.KeyMsg{Type: tea.KeyTab}
	back := tea.KeyMsg{Type: tea.KeyShiftTab}
	seen := []naSection{m.NewAgent.Section}
	for range 4 {
		m, _ = naKey(m, tab)
		seen = append(seen, m.NewAgent.Section)
	}
	require.Equal(t, []naSection{naTask, naContext, naTrust, naAgent, naTask}, seen)
	m, _ = naKey(m, back)
	require.Equal(t, naAgent, m.NewAgent.Section)
	m, _ = naKey(m, back)
	require.Equal(t, naTrust, m.NewAgent.Section)

	// 1-4 jump outside the editor; inside it they are text.
	m = naType(m, "2")
	require.Equal(t, naTask, m.NewAgent.Section)
	m = naType(m, "3")
	require.Equal(t, naTask, m.NewAgent.Section)
	require.Equal(t, "3", m.NewAgent.Task.String())
}

func TestNewAgent_EditorKeys(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naType(m, "fix the bug")
	m, _ = naKey(m, key("ctrl+w"))
	require.Equal(t, "fix the ", m.NewAgent.Task.String())
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyBackspace, Alt: true})
	require.Equal(t, "fix ", m.NewAgent.Task.String())
	m, _ = naKey(m, key(" "))
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyHome})
	m = naType(m, ">")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyEnd})
	m = naType(m, "<")
	require.Equal(t, ">fix  <", m.NewAgent.Task.String())
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyLeft})
	m, _ = naKey(m, key("backspace"))
	require.Equal(t, ">fix <", m.NewAgent.Task.String())
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = naType(m, "b")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyUp})
	require.Equal(t, 0, m.NewAgent.Task.Row)
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyDown})
	require.Equal(t, 1, m.NewAgent.Task.Row)
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyRight})
	require.Equal(t, 1, m.NewAgent.Task.Row)

	// ctrl+u clears the line; on an empty editor it clears the draft.
	m, _ = naKey(m, key("ctrl+u"))
	require.Equal(t, ">fix \n", m.NewAgent.Task.String())
	m.NewAgent.Trust = trustSession
	m.NewAgent.Task.Clear()
	m, _ = naKey(m, key("ctrl+u"))
	require.True(t, m.NewAgent.Task.Empty())
	require.Equal(t, trustAsk, m.NewAgent.Trust, "the whole draft is gone")
}

func TestNewAgent_EscKeepsDraft(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naType(m, "keep me")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlJ})
	m = naType(m, "please")
	m, cmd := naKey(m, key("esc"))
	require.Nil(t, cmd)
	require.Equal(t, FocusBoard, m.Focus)
	require.Nil(t, m.NewAgent)
	require.NotNil(t, m.newAgentDraft)

	m = naOpen(t, m)
	require.Equal(t, "keep me\nplease", m.NewAgent.Task.String())
	require.Contains(t, ansi.Strip(m.View()), "keep me")
}

func TestNewAgent_TrustKeys(t *testing.T) {
	m := naOpen(t, naModel(t))
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlT})
	require.Empty(t, m.Err)
	require.Equal(t, trustSession, m.NewAgent.Trust)
	req, err := m.NewAgent.request(spawnCWD(m), nil)
	require.NoError(t, err)
	_, req, ok := newAgentTrustRequest(m, spawnCWD(m), req)
	require.True(t, ok)
	require.True(t, req.TrustFolder)

	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlA})
	require.Equal(t, trustAlways, m.NewAgent.Trust)
	view := ansi.Strip(m.View())
	require.Contains(t, view, "always")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlA})
	require.Equal(t, trustAsk, m.NewAgent.Trust, "a second press turns it off")
	require.Contains(t, ansi.Strip(m.View()), "folder not trusted: the agent will ask")

	// ctrl+a saves the repo folder at launch, as the wizard does.
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlA})
	m, cmd := naKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	require.NotNil(t, cmd)
	require.Equal(t, "~/dev/proj", m.Store.String("auto_trust_dirs"))
}

func TestNewAgent_DefaultsFromLastSpawn(t *testing.T) {
	m := naModel(t)
	first := naOpen(t, m)
	require.Equal(t, "claude", first.NewAgent.selection().selectedTool().ID, "default_spawn_tool")

	m.lastSpawn = &quickChoice{Tool: "codex"}
	m = naOpen(t, m)
	require.Equal(t, "codex", m.NewAgent.selection().selectedTool().ID, "the last launch wins")

	// A remembered agent that is no longer offered falls back with a notice.
	m, _ = naKey(m, key("esc"))
	require.Equal(t, "codex", m.newAgentDraft.choice().Tool, "the draft keeps its agent")
	m.newAgentDraft = nil
	m.lastSpawn = &quickChoice{Tool: "grok"}
	m = naOpen(t, m)
	require.Equal(t, "claude", m.NewAgent.selection().selectedTool().ID)
	require.Contains(t, m.NewAgent.Notice, "grok isn't available")
	require.Empty(t, m.Notice, "the board never sees it")
}

func TestNewAgent_NoAgentInstalled(t *testing.T) {
	m := naModel(t)
	m.DaemonTools = daemonTools(t)
	m, _ = naKey(m, key("n"))
	require.Equal(t, FocusBoard, m.Focus)
	require.Contains(t, m.Err, "no agent is installed")
}

func TestNewAgent_OffIsInert(t *testing.T) {
	m := setFlag(t, naModel(t), "new_agent_page", false)
	for _, k := range []tea.KeyType{tea.KeyCtrlO, tea.KeyCtrlG, tea.KeyCtrlS} {
		next, cmd := naKey(m, tea.KeyMsg{Type: k})
		require.Nil(t, cmd, "%v", k)
		require.Equal(t, FocusBoard, next.Focus, "%v", k)
		require.Nil(t, next.NewAgent)
		require.Nil(t, next.newAgentDraft)
		require.Empty(t, next.Err)
		require.Empty(t, next.Notice)
	}
}

func TestNewAgent_FlagTurnedOffClosesKeepingDraft(t *testing.T) {
	m := naOpen(t, naModel(t))
	m = naType(m, "draft")
	m = setFlag(t, m, "new_agent_page", false)
	m, _ = naKey(m, key("x"))
	require.Equal(t, FocusBoard, m.Focus)
	require.Nil(t, m.NewAgent)
	require.Equal(t, "draft", m.newAgentDraft.Task.String())
}

func TestNewAgent_ViewFitsEverySize(t *testing.T) {
	for _, sz := range []goldenSize{{120, 40}, {80, 24}, {60, 20}, {59, 20}, {50, 20}, {40, 12}, {30, 8}} {
		m := naOpen(t, naModel(t))
		m.Width, m.Height = sz.w, sz.h
		m = naType(m, strings.Repeat("a long task line ", 12))
		m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlJ})
		m = naType(m, "日本語")
		lines := strings.Split(m.View(), "\n")
		require.Len(t, lines, sz.h, "%dx%d", sz.w, sz.h)
		for i, l := range lines {
			require.LessOrEqual(t, ansi.StringWidth(l), sz.w, "%dx%d line %d: %q", sz.w, sz.h, i, ansi.Strip(l))
		}
	}
}

func TestNewAgent_ViewEmptyState(t *testing.T) {
	m := naOpen(t, naModel(t))
	m.Width, m.Height = 80, 24
	out := ansi.Strip(m.View())
	for _, want := range []string{
		"NEW AGENT", "~/dev/proj", "draft", "STEPS", "✓ Agent", "▸ Task", "· Context", "? Trust", "ask",
		"AGENT", "Claude Code", "TASK", "describe the task", "CONTEXT",
		"none yet: type @path, paste, or ctrl+o to import", "TRUST",
		"folder not trusted: the agent will ask", "ctrl+t this session only", "ctrl+a always (~/dev/proj)",
		"enter launch", "ctrl+j newline", "esc close",
	} {
		require.Contains(t, out, want)
	}

	m.Width = 50
	narrow := ansi.Strip(m.View())
	require.Contains(t, narrow, "‹ Agent · [Task] · Context · Trust ›")
	require.NotContains(t, narrow, "STEPS")
}

func TestNewAgent_ViewTrustAndNoticeWords(t *testing.T) {
	m := naModel(t)
	m.lastSpawn = &quickChoice{Tool: "grok"}
	m = naOpen(t, m)
	m.Width, m.Height = 80, 24
	require.Contains(t, ansi.Strip(m.View()), "! grok isn't available; using claude")

	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlT})
	out := ansi.Strip(m.View())
	require.Contains(t, out, "✓ Trust   sess")
	require.Contains(t, out, "trusted for this session only")
	m.Width = 50
	require.Contains(t, ansi.Strip(m.View()), "trust: sess")

	m.Width = 80
	cwd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, m.Store.Set("auto_trust_dirs", cwd))
	out = ansi.Strip(m.View())
	require.Contains(t, out, "trusted (auto_trust_dirs)")
	for _, l := range strings.Split(out, "\n") {
		if rail, _, ok := strings.Cut(l, "│"); ok && strings.Contains(rail, "Trust") {
			require.Contains(t, rail, "Trust trusted", "the rail column says it whole")
		}
	}
}

func TestNewAgent_RailWordsWhole(t *testing.T) {
	st := &NewAgentState{Section: naTrust}
	for _, tv := range []naTrustView{
		{mark: "?", word: "ask"},
		{mark: "✓", word: "sess", ok: true},
		{mark: "✓", word: "always", ok: true},
		{mark: "✓", word: "trusted", ok: true},
	} {
		for _, sec := range []naSection{naTask, naTrust} {
			st.Section = sec
			rail := naRail(st, tv)
			found := false
			for _, l := range rail {
				plain := ansi.Strip(l)
				require.LessOrEqual(t, ansi.StringWidth(plain), naRailW, "%q", plain)
				require.NotContains(t, plain, "…")
				if strings.HasSuffix(plain, tv.word) && strings.Contains(plain, "Trust") {
					found = true
				}
			}
			require.True(t, found, "rail carries %q whole", tv.word)
		}
	}
}
