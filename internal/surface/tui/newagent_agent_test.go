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

// naAgentOpen opens the page and goes to its agent step with ctrl+g.
func naAgentOpen(t *testing.T, m Model) Model {
	t.Helper()
	m = naOpen(t, m)
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyCtrlG})
	require.Equal(t, naStepAgent, m.NewAgent.Step)
	return m
}

// naModelOpen is naAgentOpen then enter: the model step.
func naModelOpen(t *testing.T, m Model) Model {
	t.Helper()
	m = naAgentOpen(t, m)
	m, _ = naKey(m, key("enter"))
	require.Equal(t, naStepModel, m.NewAgent.Step)
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

func TestAgentStep_CtrlGEnterAndEsc(t *testing.T) {
	m := naAgentOpen(t, naModel(t))
	require.Contains(t, naView(m), "› agent")
	m, cmd := naKey(m, key("enter"))
	require.Nil(t, cmd, "enter never launches")
	require.Equal(t, naStepModel, m.NewAgent.Step, "enter confirms the agent and goes to the model")
	require.Contains(t, naView(m), "✓ agent")

	// The page opened on the task; on an earlier step esc goes back one step
	// (R21 K1, decided 2026-11-19), and on the first step it closes the page.
	m, _ = naKey(m, key("esc"))
	require.Equal(t, FocusNewAgent, m.Focus, "esc on a step before the opened one goes back")
	require.Equal(t, naStepAgent, m.NewAgent.Step)
	m, _ = naKey(m, key("esc"))
	require.Equal(t, FocusBoard, m.Focus, "esc on the first step closes the page")
	require.NotNil(t, m.newAgentDraft, "the draft is kept")
}

func TestAgentStep_ListsMissingDimmedUnselectable(t *testing.T) {
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

func TestAgentStep_TabLeavesTheMissingHeader(t *testing.T) {
	m := naModel(t)
	m.DaemonTools = daemonTools(t, "claude", "cursor")
	m = naAgentOpen(t, m)
	for range 5 {
		m, _ = naKey(m, key("j"))
	}
	require.True(t, m.NewAgent.OnMissing)
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyTab})
	require.False(t, m.NewAgent.OnMissing)
	require.Equal(t, naStepModel, m.NewAgent.Step)
	require.Nil(t, m.Settings)
}

func TestModelStep_CtrlSLaunchesWhileFiltering(t *testing.T) {
	m := naModel(t)
	m.DaemonTools = cursorModels(t)
	m = naModelOpen(t, m)
	m, _ = naKey(m, key("/"))
	require.True(t, m.NewAgent.Filtering)
	m, cmd := naKey(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	require.NotNil(t, cmd, "ctrl+s launches while the filter is active")
	require.True(t, m.NewAgent.Launching)
	require.False(t, m.NewAgent.Filtering)
	require.Contains(t, naView(m), "starting…", "the progress shows under the current line")
}

func TestModelStep_FilterKeepsCursorByID(t *testing.T) {
	m := naModel(t)
	m.DaemonTools = cursorModels(t)
	m = naAgentOpen(t, m)
	m = naPickTool(t, m, "cursor")
	m, _ = naKey(m, key("enter"))
	require.Equal(t, naStepModel, m.NewAgent.Step)
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
	require.Equal(t, naStepModel, m.NewAgent.Step, "the first enter does not leave the step")

	require.Equal(t, "clear", naEscWord(m.NewAgent), "the footer says what esc does with a kept filter")
	m, _ = naKey(m, key("esc"))
	require.Equal(t, "", m.NewAgent.Filter, "esc clears the kept filter first")
	require.Equal(t, naStepModel, m.NewAgent.Step)
	require.Equal(t, "sonnet-4.6-thinking", m.NewAgent.choice().Model)
	require.Equal(t, "back", naEscWord(m.NewAgent))
	m, _ = naKey(m, key("esc"))
	require.Equal(t, FocusNewAgent, m.Focus, "then one step back, even before the step the page opened on")
	require.Equal(t, naStepAgent, m.NewAgent.Step)
	require.Equal(t, "close", naEscWord(m.NewAgent), "nothing before the agent step")
	m, _ = naKey(m, key("esc"))
	require.Equal(t, FocusBoard, m.Focus, "on the first step esc closes the page")
	require.NotNil(t, m.newAgentDraft, "the draft is kept")
}

func TestModelStep_EscClearsTypedFilterThenGoesBack(t *testing.T) {
	m := naModel(t)
	m.DaemonTools = cursorModels(t)
	m = naModelOpen(t, m)
	m, _ = naKey(m, key("/"))
	m = naType(m, "son")
	m, _ = naKey(m, key("esc"))
	require.Empty(t, m.NewAgent.Filter, "esc clears the typed text")
	require.False(t, m.NewAgent.Filtering)
	require.Equal(t, naStepModel, m.NewAgent.Step)
	m, _ = naKey(m, key("enter"))
	require.NotEqual(t, naStepModel, m.NewAgent.Step, "a second enter goes next")
}

func TestModelStep_PasteTypesFlat(t *testing.T) {
	m := naModel(t)
	m.DaemonTools = cursorModels(t)
	m = naModelOpen(t, m)
	m, _ = naKey(m, key("/"))
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("so\nn\x1b[31m"), Paste: true})
	require.Equal(t, "so n[31m", m.NewAgent.Filter)
	require.Equal(t, naStepModel, m.NewAgent.Step, "a paste never advances")
}

func TestModelStep_FilterMatchesName(t *testing.T) {
	m := naModelOpen(t, naModel(t))
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

func TestModelStep_EffortBracketed(t *testing.T) {
	m := naModelOpen(t, naModel(t))
	require.Contains(t, naView(m), "["+m.NewAgent.choice().Effort+"]")
	for range 6 {
		m, _ = naKey(m, key("-"))
	}
	require.Equal(t, "low", m.NewAgent.choice().Effort, "- stops at the first option")
	m, _ = naKey(m, key("+"))
	m, _ = naKey(m, key("l"))
	require.Equal(t, "high", m.NewAgent.choice().Effort)
	m, _ = naKey(m, key("h"))
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyRight})
	v := naView(m)
	require.Contains(t, v, "[high]")
	require.Contains(t, v, "effort  low  medium  [high]  xhigh  max")

	// A model with no effort has no effort row and its done line drops it.
	m = naModel(t)
	m.DaemonTools = cursorModels(t)
	m = naPickTool(t, naAgentOpen(t, m), "cursor")
	m, _ = naKey(m, key("enter"))
	require.NotContains(t, naView(m), "[", "no effort row, so nothing is bracketed")
	m, _ = naKey(m, key("enter")) // skills do not apply: the task
	require.Equal(t, naStepTask, m.NewAgent.Step)
	require.NotContains(t, naView(m), "· effort")
}

func TestSkillsStep_OnlyWithSkillArgs(t *testing.T) {
	m := naModel(t)
	home := filepathHome(t)
	writeHomeSkill(t, home, "go-testing", "table-driven tests and goldens")
	writeHomeSkill(t, home, "release", "cut a release branch")
	m.DaemonTools = daemonTools(t, "claude", "cursor")
	m = naModelOpen(t, m)
	m = naPickToolOnAgentStep(t, m, "claude")
	m, _ = naKey(m, key("enter")) // model
	m, _ = naKey(m, key("enter")) // skills
	require.Equal(t, naStepSkills, m.NewAgent.Step)
	require.Contains(t, naView(m), "› skills")
	m, _ = naKey(m, key(" "))
	require.Equal(t, []string{"go-testing"}, m.NewAgent.selection().chosenSkills())
	require.Contains(t, naView(m), "[x] go-testing")
	m, _ = naKey(m, key("j"))
	m, _ = naKey(m, key(" "))
	m, _ = naKey(m, key(" "))
	require.Equal(t, []string{"go-testing"}, m.NewAgent.selection().chosenSkills(), "space toggles the row under the cursor")
	m, _ = naKey(m, key("enter"))
	require.Equal(t, naStepTask, m.NewAgent.Step)
	require.Contains(t, naView(m), "✓ skills   go-testing")

	// Cursor declares no skill_args: the step is skipped both ways.
	m, _ = naKey(m, key("ctrl+g"))
	m = naPickTool(t, m, "cursor")
	m, _ = naKey(m, key("enter")) // model
	m, _ = naKey(m, key("enter"))
	require.Equal(t, naStepTask, m.NewAgent.Step, "enter skips the skills step")
	require.Nil(t, m.NewAgent.selection().chosenSkills())
	require.Contains(t, naView(m), "– skills   not available")
	m, _ = naKey(m, tea.KeyMsg{Type: tea.KeyShiftTab})
	require.Equal(t, naStepModel, m.NewAgent.Step, "shift+tab skips it too")
}

// naPickToolOnAgentStep goes to the agent step and picks tool id there.
func naPickToolOnAgentStep(t *testing.T, m Model, id string) Model {
	t.Helper()
	m, _ = naKey(m, key("ctrl+g"))
	return naPickTool(t, m, id)
}

// filepathHome is the test's HOME (naModel set it).
func filepathHome(t *testing.T) string {
	t.Helper()
	home, err := filepath.Abs(os.Getenv("HOME"))
	require.NoError(t, err)
	return home
}

func TestAgentStep_RefreshPickersKeepsSelection(t *testing.T) {
	m := naModel(t)
	m.DaemonTools = daemonTools(t, "claude", "codex", "cursor")
	m = naAgentOpen(t, m)
	m = naPickTool(t, m, "codex")
	m, _ = naKey(m, key("enter"))
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
	require.Equal(t, naStepModel, m.NewAgent.Step)
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
	require.Empty(t, m.NewAgent.Notice)

	m = send(m, daemonTools(t, "cursor"))
	require.Equal(t, "cursor", m.NewAgent.choice().Tool, "a tool that went away falls back")
	require.Contains(t, m.NewAgent.Notice, "codex isn't available", "with a notice")

	m = send(m, daemonTools(t))
	require.Equal(t, FocusBoard, m.Focus)
	require.Nil(t, m.NewAgent)
	require.NotNil(t, m.newAgentDraft, "the draft is kept")
	require.Contains(t, m.Notice, "no agent is available")
}

// A refresh that leaves the current step without anything to show moves on
// to the next step that applies (S2).
func TestSkillsStep_RefreshMovesOffASkippedStep(t *testing.T) {
	m := naModel(t)
	writeHomeSkill(t, filepathHome(t), "go-testing", "tests")
	m.DaemonTools = daemonTools(t, "claude", "cursor")
	m = naAgentOpen(t, m)
	m, _ = naKey(m, key("enter"))
	m, _ = naKey(m, key("enter"))
	require.Equal(t, naStepSkills, m.NewAgent.Step)
	data, err := json.Marshal(protocol.ToolsChanged{Tools: daemonTools(t, "cursor")})
	require.NoError(t, err)
	m = m.applyEvent(protocol.Envelope{Type: protocol.EventToolsChanged, Data: data})
	require.Equal(t, naStepTask, m.NewAgent.Step)
}

func TestModelStep_FitsEverySize(t *testing.T) {
	m := naModel(t)
	m.DaemonTools = cursorModels(t)
	m = naAgentOpen(t, m)
	m = naPickTool(t, m, "cursor")
	m, _ = naKey(m, key("enter"))
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
}
