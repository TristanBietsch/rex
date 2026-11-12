package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// naAgentScreen opens the page and its Agent screen with ctrl+g.
func naAgentOpen(t *testing.T, m Model) Model {
	t.Helper()
	m = naOpen(t, m)
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlG})
	require.Equal(t, naAgentScreen, m.NewAgent.Screen)
	return m
}

// naPickTool moves the agent cursor until tool id is selected.
func naPickTool(t *testing.T, m Model, id string) Model {
	t.Helper()
	for range len(m.NewAgent.Tools) + 1 {
		if m.NewAgent.choice().Tool == id {
			return m
		}
		m, _ = naKey(m, key("j"))
	}
	require.Equal(t, id, m.NewAgent.choice().Tool)
	return m
}

func naView(m Model) string { return ansi.Strip(m.View()) }

// cursorModels is the daemon tool list with claude and cursor installed and
// cursor reporting 200 detected models, three of them matching "son".
func cursorModels(t *testing.T) []protocol.ToolInfo {
	t.Helper()
	infos := daemonTools(t, "claude", "cursor")
	for i := range infos {
		if infos[i].ID != "cursor" {
			continue
		}
		models := []protocol.ModelInfo{
			{ID: "auto", Name: "Auto", Detected: true},
			{ID: "sonnet-4.6", Name: "sonnet-4.6", Detected: true},
			{ID: "sonnet-4.6-thinking", Name: "sonnet-4.6-thinking", Detected: true},
			{ID: "claude-4.5-sonnet", Name: "claude-4.5-sonnet", Detected: true},
		}
		for n := len(models); n < 200; n++ {
			id := fmt.Sprintf("model-%03d", n)
			models = append(models, protocol.ModelInfo{ID: id, Name: id, Detected: true})
		}
		infos[i].Models = models
	}
	return infos
}

func TestAgentScreen_CtrlGAndEnterOpenEscBack(t *testing.T) {
	m := naAgentOpen(t, naModel(t))
	m, _ = naKey(m, key("esc"))
	require.Equal(t, naCompose, m.NewAgent.Screen)
	require.Equal(t, FocusNewAgent, m.Focus, "esc on the Agent screen goes back to Compose")

	m.NewAgent.Section = naAgent // the agent row
	m, cmd := naKey(m, key("enter"))
	require.Nil(t, cmd, "enter on the agent row never launches")
	require.Equal(t, naAgentScreen, m.NewAgent.Screen)
	require.Contains(t, naView(m), "NEW AGENT › AGENT")

	m, _ = naKey(m, key("enter")) // done
	require.Equal(t, naCompose, m.NewAgent.Screen)
	require.Equal(t, FocusNewAgent, m.Focus)
}

func TestAgentScreen_ListsMissingDimmedUnselectable(t *testing.T) {
	m := naModel(t)
	m.DaemonTools = daemonTools(t, "claude", "cursor")
	m = naAgentOpen(t, m)
	st := m.NewAgent
	require.NotEmpty(t, st.Missing)
	installed := map[string]bool{"claude": true, "cursor": true}
	for range 20 {
		m, _ = naKey(m, key("j"))
		require.True(t, installed[m.NewAgent.choice().Tool], "j never lands on a missing agent")
	}
	require.True(t, m.NewAgent.OnMissing, "j stops on the not installed header")
	v := naView(m)
	require.Contains(t, v, "not installed")
	require.Contains(t, v, "· "+st.Missing[0].Tool.Name[:4])

	for range 5 {
		m, _ = naKey(m, key("k"))
	}
	require.False(t, m.NewAgent.OnMissing)
	require.Equal(t, "claude", m.NewAgent.choice().Tool)

	// enter on the header opens Settings → Onboarding, the draft kept.
	for range 5 {
		m, _ = naKey(m, key("j"))
	}
	require.True(t, m.NewAgent.OnMissing)
	m, _ = naKey(m, key("enter"))
	require.Equal(t, FocusSettings, m.Focus)
	require.NotNil(t, m.Settings)
	require.True(t, strings.HasPrefix(m.Settings.CursorID, onboardPrefix), m.Settings.CursorID)
	require.NotNil(t, m.newAgentDraft, "the draft survives the detour")
}

func TestAgentScreen_OnMissingClearsOnPaneChange(t *testing.T) {
	m := naModel(t)
	m.DaemonTools = daemonTools(t, "claude", "cursor")
	m = naAgentOpen(t, m)
	for range 5 {
		m, _ = naKey(m, key("j"))
	}
	require.True(t, m.NewAgent.OnMissing)
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyTab})
	require.False(t, m.NewAgent.OnMissing)
	require.Equal(t, naPaneModels, m.NewAgent.Pane)
	m, _ = naKey(m, key("enter"))
	require.NotEqual(t, FocusSettings, m.Focus, "enter off the agents pane returns to Compose")
	require.Nil(t, m.Settings)
	require.NotContains(t, naView(m), "k agents")
}

func TestAgentScreen_CtrlSLaunchesWhileFiltering(t *testing.T) {
	m := naModel(t)
	m.DaemonTools = cursorModels(t)
	m = naAgentOpen(t, m)
	m, _ = naKey(m, key("/"))
	require.True(t, m.NewAgent.Filtering)
	m, cmd := naKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	require.NotNil(t, cmd, "ctrl+s launches while the filter is active")
	require.Nil(t, m.NewAgent)
}

func TestAgentScreen_ModelFilterKeepsCursorByID(t *testing.T) {
	m := naModel(t)
	m.DaemonTools = cursorModels(t)
	m = naAgentOpen(t, m)
	m = naPickTool(t, m, "cursor")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyTab})
	require.Equal(t, naPaneModels, m.NewAgent.Pane)
	m, _ = naKey(m, key("j"))
	m, _ = naKey(m, key("j"))
	require.Equal(t, "sonnet-4.6-thinking", m.NewAgent.choice().Model)

	m, _ = naKey(m, key("/"))
	require.True(t, m.NewAgent.Filtering)
	m = naType(m, "SoN")
	got := m.NewAgent.filteredModels()
	require.Len(t, got, 3, "case-insensitive substring on id and name")
	for _, md := range got {
		require.Contains(t, strings.ToLower(md.ID), "son")
	}
	require.Equal(t, "sonnet-4.6-thinking", m.NewAgent.choice().Model, "the cursor stays on the same id")
	require.Contains(t, naView(m), "200 · 3 shown")

	// j/k are letters while typing; arrows move.
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyDown})
	require.Equal(t, "claude-4.5-sonnet", m.NewAgent.choice().Model)
	m, _ = naKey(m, key("backspace"))
	require.Equal(t, "So", m.NewAgent.Filter)

	// A filter that drops the current model moves to the first match.
	m = naType(m, "nnet-4.6-t")
	require.Equal(t, "sonnet-4.6-thinking", m.NewAgent.choice().Model)
	m, _ = naKey(m, key("enter"))
	require.False(t, m.NewAgent.Filtering, "enter keeps the filter, stops typing")
	require.Equal(t, naAgentScreen, m.NewAgent.Screen)

	m, _ = naKey(m, key("esc"))
	require.Equal(t, "", m.NewAgent.Filter, "esc clears the filter first")
	require.Equal(t, naAgentScreen, m.NewAgent.Screen)
	require.Equal(t, "sonnet-4.6-thinking", m.NewAgent.choice().Model)
	m, _ = naKey(m, key("esc"))
	require.Equal(t, naCompose, m.NewAgent.Screen)
}

func TestAgentScreen_FilterMatchesName(t *testing.T) {
	m := naAgentOpen(t, naModel(t))
	m = naPickTool(t, m, "claude")
	m, _ = naKey(m, key("/"))
	m = naType(m, "opus 4")
	got := m.NewAgent.filteredModels()
	require.Len(t, got, 1)
	require.Equal(t, "opus", got[0].ID)
	// Control characters never reach the filter.
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\x1b[31m\n")})
	require.Equal(t, "opus 4[31m", m.NewAgent.Filter)
}

func TestAgentScreen_EffortBracketed(t *testing.T) {
	m := naAgentOpen(t, naModel(t))
	m = naPickTool(t, m, "claude")
	require.Contains(t, naView(m), "["+m.NewAgent.choice().Effort+"]")
	for range 6 {
		m, _ = naKey(m, key("h"))
	}
	require.Equal(t, "low", m.NewAgent.choice().Effort, "h stops at the first option")
	m, _ = naKey(m, key("l"))
	m, _ = naKey(m, key("l"))
	require.Equal(t, "high", m.NewAgent.choice().Effort)
	v := naView(m)
	require.Contains(t, v, "[high]")
	require.Contains(t, v, "low  medium  [high]  xhigh  max")

	m = naModel(t)
	m.DaemonTools = cursorModels(t)
	m = naPickTool(t, naAgentOpen(t, m), "cursor")
	require.Contains(t, naView(m), "(none for this model)")
}

func TestAgentScreen_SkillsOnlyWithSkillArgs(t *testing.T) {
	m := naModel(t)
	home := filepathHome(t)
	writeHomeSkill(t, home, "go-testing", "table-driven tests and goldens")
	writeHomeSkill(t, home, "release", "cut a release branch")
	m.DaemonTools = daemonTools(t, "claude", "cursor")
	m = naAgentOpen(t, m)
	m = naPickTool(t, m, "claude")
	require.Contains(t, naView(m), "SKILLS")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyTab})
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyTab})
	require.Equal(t, naPaneSkills, m.NewAgent.Pane)
	m, _ = naKey(m, key(" "))
	require.Equal(t, []string{"go-testing"}, m.NewAgent.selection().chosenSkills())
	require.Contains(t, naView(m), "[x] go-testing")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyTab})
	require.Equal(t, naPaneAgents, m.NewAgent.Pane)

	m = naPickTool(t, m, "cursor")
	require.NotContains(t, naView(m), "SKILLS", "cursor declares no skill_args")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyTab})
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyTab})
	require.Equal(t, naPaneAgents, m.NewAgent.Pane, "tab skips the skills pane")
	require.Nil(t, m.NewAgent.selection().chosenSkills())
}

// filepathHome is the test's HOME (naModel set it).
func filepathHome(t *testing.T) string {
	t.Helper()
	home, err := filepath.Abs(os.Getenv("HOME"))
	require.NoError(t, err)
	return home
}

func TestAgentScreen_RefreshPickersKeepsSelection(t *testing.T) {
	m := naModel(t)
	m.DaemonTools = daemonTools(t, "claude", "codex", "cursor")
	m = naAgentOpen(t, m)
	m = naPickTool(t, m, "codex")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyTab})
	m, _ = naKey(m, key("j"))
	m, _ = naKey(m, key("l"))
	want := m.NewAgent.choice()
	require.Equal(t, "codex", want.Tool)

	send := func(m Model, infos []protocol.ToolInfo) Model {
		data, err := json.Marshal(protocol.ToolsChanged{Tools: infos})
		require.NoError(t, err)
		return m.applyEvent(protocol.Envelope{Type: protocol.EventToolsChanged, Data: data})
	}
	m = send(m, daemonTools(t, "codex", "cursor"))
	require.Equal(t, FocusNewAgent, m.Focus)
	require.Equal(t, naAgentScreen, m.NewAgent.Screen)
	require.Equal(t, want, m.NewAgent.choice(), "tool, model and effort kept by id")
	require.Equal(t, "codex", m.NewAgent.Tools[m.NewAgent.ToolIdx].ID)
	require.True(t, func() bool {
		for _, r := range m.NewAgent.Missing {
			if r.Tool.ID == "claude" {
				return true
			}
		}
		return false
	}(), "claude moved to not installed")

	m = send(m, daemonTools(t, "cursor"))
	require.Equal(t, "cursor", m.NewAgent.choice().Tool, "a tool that went away falls back")

	m = send(m, daemonTools(t))
	require.Equal(t, FocusBoard, m.Focus)
	require.Nil(t, m.NewAgent)
	require.NotNil(t, m.newAgentDraft, "the draft is kept")
	require.Contains(t, m.Notice, "no agent is available")
}

func TestAgentScreen_FitsEverySize(t *testing.T) {
	m := naModel(t)
	m.DaemonTools = cursorModels(t)
	m = naAgentOpen(t, m)
	m = naPickTool(t, m, "cursor")
	m, _ = naKey(m, key("/"))
	m = naType(m, "son")
	for _, sz := range []goldenSize{{120, 40}, {80, 24}, {60, 20}, {59, 20}, {50, 20}, {40, 12}} {
		m.Width, m.Height = sz.w, sz.h
		lines := strings.Split(m.View(), "\n")
		require.Len(t, lines, sz.h, "%dx%d", sz.w, sz.h)
		for i, l := range lines {
			require.LessOrEqual(t, ansi.StringWidth(l), sz.w, "%dx%d line %d: %q", sz.w, sz.h, i, ansi.Strip(l))
		}
	}
	m.Width, m.Height = 50, 20
	require.Contains(t, naView(m), "‹ Agents · [Models] · Skills ›")
}
