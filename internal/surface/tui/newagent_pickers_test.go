package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

func TestNewAgent_OffersInstalledToolsOnly(t *testing.T) {
	m := pickerModel(t, "claude", "qwen")
	m, _ = executeCommand(m, "new")
	require.Equal(t, FocusNewAgent, m.Focus)
	require.Equal(t, []string{"claude", "qwen"}, toolIDs(m.NewAgent.Tools))
	m = naAt(m, naStepAgent)
	out := ansi.Strip(m.View())
	require.Contains(t, out, "Claude Code")
	require.Contains(t, out, "Qwen Code")
	require.NotContains(t, out, "Grok Build\n", "an agent that is not installed is only in the dim list")
	require.NotContains(t, out, "▸ ✻ OpenAI Codex")

	// Nothing installed: the page refuses and points at Onboarding.
	none := pickerModel(t)
	none, _ = executeCommand(none, "new")
	require.Equal(t, FocusBoard, none.Focus)
	require.Equal(t, "new agent: no agent is installed (see Settings → Onboarding)", none.Err)
}

// With every agent installed the agent step lists Qwen Code, Cursor CLI and
// Grok Build after the others, in registry order; one that is not installed
// moves to Settings → Onboarding like claude or codex would.
func TestNewAgent_OffersExtraAgentsWhenInstalled(t *testing.T) {
	all := []string{"claude", "codex", "antigravity", "ollama", "qwen", "cursor", "grok"}
	m := pickerModel(t, all...)
	m, _ = executeCommand(m, "new")
	require.Equal(t, FocusNewAgent, m.Focus)
	require.Equal(t, all, toolIDs(m.NewAgent.Tools))
	m = naAt(m, naStepAgent)
	out := ansi.Strip(m.View())
	for _, name := range []string{"Qwen Code", "Cursor CLI", "Grok Build"} {
		require.Contains(t, out, name)
	}

	m = pickerModel(t, "claude", "codex", "qwen", "grok")
	reg, err := registry.Load("")
	require.NoError(t, err)
	offer, missing := pickerTools(m, reg)
	require.Equal(t, []string{"claude", "codex", "qwen", "grok"}, toolIDs(offer))
	var missingIDs []string
	for _, r := range missing {
		missingIDs = append(missingIDs, r.Tool.ID)
	}
	require.Contains(t, missingIDs, "cursor")
	for _, r := range missing {
		if r.Tool.ID == "cursor" {
			require.Contains(t, r.setting().Help, "cursor-agent", "the hint names the binary to install")
		}
	}
}

func TestNewAgent_ModelStepMarksDetectedModels(t *testing.T) {
	m := pickerModel(t, "claude", "qwen")
	m, _ = executeCommand(m, "new")
	m = naAt(m, naStepAgent)
	m = sendKey(m, key("j")) // qwen
	require.Equal(t, "qwen", m.NewAgent.selection().selectedTool().ID)
	m = sendKey(m, key("enter"))
	require.Equal(t, naStepModel, m.NewAgent.Step)
	out := ansi.Strip(m.View())
	require.Regexp(t, `qwen3-coder-plus\s+detected`, out)
	require.Regexp(t, `qwen3-max\s+detected`, out)
	require.NotRegexp(t, `Default model\s+detected`, out, "a built-in model is not marked")
}

// An open page picks up ToolsChanged without losing its place.
func TestNewAgent_ToolsChangedUpdatesInPlace(t *testing.T) {
	m := pickerModel(t, "claude", "qwen")
	m, _ = executeCommand(m, "new")
	m = naAt(m, naStepAgent)
	m = sendKey(m, key("j"))
	m = sendKey(m, key("enter"))
	m = sendKey(m, key("j")) // the second qwen model
	require.Equal(t, "qwen3-max", m.NewAgent.selection().selectedModel().ID)

	// A new model is detected, and grok appears (installed meanwhile).
	infos := daemonTools(t, "claude", "qwen", "grok")
	for i := range infos {
		if infos[i].ID == "qwen" {
			infos[i].Models = append([]protocol.ModelInfo{{ID: "qwen3-new", Name: "qwen3-new", Detected: true}}, infos[i].Models...)
		}
	}
	data, err := json.Marshal(protocol.ToolsChanged{Tools: infos})
	require.NoError(t, err)
	m = m.applyEvent(protocol.Envelope{Type: protocol.EventToolsChanged, Data: data})

	require.Equal(t, []string{"claude", "qwen", "grok"}, toolIDs(m.NewAgent.Tools))
	require.Equal(t, naStepModel, m.NewAgent.Step, "still on the model step")
	require.Equal(t, "qwen", m.NewAgent.selection().selectedTool().ID, "the cursor stays on the same tool")
	require.Equal(t, "qwen3-max", m.NewAgent.selection().selectedModel().ID, "and the same model, found by id")
	require.Contains(t, ansi.Strip(m.View()), "qwen3-new")
}

func TestNewAgent_LongModelListScrollsToTheCursor(t *testing.T) {
	m := pickerModel(t, "claude", "qwen")
	infos := daemonTools(t, "claude", "qwen")
	for i := range infos {
		if infos[i].ID != "qwen" {
			continue
		}
		infos[i].Models = nil
		for n := 0; n < 40; n++ {
			infos[i].Models = append(infos[i].Models, protocol.ModelInfo{ID: "m" + strings.Repeat("x", n%3) + string(rune('a'+n%26)) + string(rune('a'+n/26)), Detected: true})
		}
	}
	m.DaemonTools = infos
	m, _ = executeCommand(m, "new")
	m = naAt(m, naStepAgent)
	m = sendKey(m, key("j"))
	m = sendKey(m, key("enter"))
	for range 30 {
		m = sendKey(m, key("j"))
	}
	want := m.NewAgent.selection().selectedModel().Name
	require.Contains(t, ansi.Strip(m.View()), want, "the selected model stays on screen")
	require.Contains(t, ansi.Strip(m.View()), "↑ more")
}

// A saved default tool that is hidden falls back to the first offered tool
// with a notice on the page (the board never sees it).
func TestNewAgent_NoticeWhenItsDefaultToolIsHidden(t *testing.T) {
	m := quickModel(t, "claude", "qwen")
	m, _ = executeCommand(m, "new")
	require.Equal(t, "claude isn't available; using qwen", m.NewAgent.Notice)
	require.Empty(t, m.Notice)
	require.Equal(t, "qwen", m.NewAgent.selection().selectedTool().ID)
	require.Equal(t, naStepAgent, m.NewAgent.Step, "a notice opens on the agent step")

	m = quickModel(t, "qwen", "qwen")
	m, _ = executeCommand(m, "new")
	require.Empty(t, m.NewAgent.Notice)
	require.Equal(t, naStepTask, m.NewAgent.Step)
}

// ToolsChanged while on the model step: the model the cursor was on may have
// no effort block any more. The page keeps going and draws no effort row.
func TestNewAgent_ToolsChangedDropsTheEffortRow(t *testing.T) {
	m := pickerModel(t, "claude", "qwen")
	m, _ = executeCommand(m, "new")
	m = naAt(m, naStepModel)
	require.Equal(t, "claude", m.NewAgent.selection().selectedTool().ID)
	require.Regexp(t, `effort  +low`, ansi.Strip(m.View()))

	infos := daemonTools(t, "claude", "qwen")
	for i := range infos {
		if infos[i].ID == "claude" {
			infos[i].Models = []protocol.ModelInfo{{ID: "plain", Name: "plain", Detected: true}}
		}
	}
	data, err := json.Marshal(protocol.ToolsChanged{Tools: infos})
	require.NoError(t, err)
	require.NotPanics(t, func() {
		m = m.applyEvent(protocol.Envelope{Type: protocol.EventToolsChanged, Data: data})
		_ = m.View()
		m = sendKey(m, key("j"))
		m = sendKey(m, key("k"))
	})
	require.Empty(t, m.NewAgent.selection().currentEffort())
	require.NotRegexp(t, `effort  +\S`, ansi.Strip(m.View()))
}
