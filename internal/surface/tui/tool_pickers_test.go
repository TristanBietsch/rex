package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// daemonTools is what the daemon would send: the built-in registry with the
// given ids installed. qwen also reports two detected models.
func daemonTools(t *testing.T, installed ...string) []protocol.ToolInfo {
	t.Helper()
	reg, err := registry.Load("")
	require.NoError(t, err)
	var out []protocol.ToolInfo
	for _, tool := range reg.Tools {
		info := protocol.ToolInfo{ID: tool.ID, Installed: slices.Contains(installed, tool.ID)}
		if tool.ID == "qwen" {
			info.Models = []protocol.ModelInfo{
				{ID: "qwen3-coder-plus", Name: "qwen3-coder-plus", Detected: true},
				{ID: "qwen3-max", Name: "qwen3-max", Detected: true},
			}
		}
		for _, md := range tool.Models {
			info.Models = append(info.Models, protocol.ModelInfo{ID: md.ID, Name: md.Name})
		}
		out = append(out, info)
	}
	return out
}

func toolIDs(tools []registry.Tool) []string {
	var ids []string
	for _, t := range tools {
		ids = append(ids, t.ID)
	}
	return ids
}

func pickerModel(t *testing.T, installed ...string) Model {
	t.Helper()
	m := wizardFlag(t, goldenBoard(t, goldenSize{120, 40}, false)) // :new opens the wizard
	m.DaemonTools, m.toolsKnown = daemonTools(t, installed...), true
	return m
}

func TestPickerTools_DaemonSaysWhatIsInstalled(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)

	// A daemon that predates the list: every enabled tool counts as installed.
	old := goldenBoard(t, goldenSize{120, 40}, false)
	offer, missing := pickerTools(old, reg)
	require.Empty(t, missing)
	require.Equal(t, []string{"claude", "codex", "antigravity", "ollama", "qwen", "cursor", "grok"}, toolIDs(offer),
		"qwen, cursor and grok are enabled by default now")

	m := pickerModel(t, "claude", "qwen")
	offer, missing = pickerTools(m, reg)
	require.Equal(t, []string{"claude", "qwen"}, toolIDs(offer))
	var missingIDs []string
	for _, r := range missing {
		missingIDs = append(missingIDs, r.Tool.ID)
	}
	require.Equal(t, []string{"codex", "antigravity", "ollama", "cursor", "grok"}, missingIDs)

	// A tool the daemon does not list at all is not offered either.
	m.DaemonTools = m.DaemonTools[:1]
	offer, _ = pickerTools(m, reg)
	for _, id := range toolIDs(offer) {
		require.Contains(t, []string{"echo"}, id, "only what the daemon lists and finds installed")
	}
}

func TestPickerTools_ExtraAgentsOffNeverShow(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	for name, installed := range map[string][]string{"installed": {"claude", "qwen", "cursor", "grok"}, "unknown daemon": nil} {
		t.Run(name, func(t *testing.T) {
			m := pickerModel(t, installed...)
			if installed == nil {
				m.DaemonTools, m.toolsKnown = nil, false
			}
			require.NoError(t, m.Store.Set("extra_agents", false))
			offer, missing := pickerTools(m, reg)
			for _, id := range registry.ExtraAgentIDs {
				require.NotContains(t, toolIDs(offer), id)
				for _, r := range missing {
					require.NotEqual(t, id, r.Tool.ID, "not even in Onboarding")
				}
			}
			require.Contains(t, toolIDs(offer), "claude")
		})
	}
}

func TestOverlayDaemonTools_MergedModels(t *testing.T) {
	reg, err := registry.Load("")
	require.NoError(t, err)
	qwenStatic, _ := reg.Find("qwen")
	m := pickerModel(t, "qwen")
	out, installed := overlayDaemonTools(reg, m.DaemonTools)
	require.True(t, installed["qwen"])
	require.False(t, installed["claude"])

	qwen, _ := out.Find("qwen")
	require.Equal(t, "qwen3-coder-plus", qwen.Models[0].ID, "detected first")
	require.True(t, qwen.Models[0].Detected)
	require.Equal(t, qwenStatic.ModelArgsFor("qwen3-coder-plus"), qwen.Models[0].Args, "a detected model gets the tool's model arguments")
	require.Equal(t, qwenStatic.ModelEffort, qwen.Models[0].Effort)
	last := qwen.Models[len(qwen.Models)-1]
	require.Equal(t, qwenStatic.Models[0].ID, last.ID)
	require.False(t, last.Detected)
	require.Equal(t, qwenStatic.Models[0].Effort, last.Effort, "a static model keeps its own definition")

	// The registry the TUI loaded is not changed.
	again, _ := reg.Find("qwen")
	require.Equal(t, qwenStatic.Models, again.Models)
}

func TestWizard_OffersInstalledToolsOnly(t *testing.T) {
	m := pickerModel(t, "claude", "qwen")
	m, _ = executeCommand(m, "new")
	require.Equal(t, FocusWizard, m.Focus)
	require.Equal(t, []string{"claude", "qwen"}, toolIDs(m.Wizard.Tools))
	out := ansi.Strip(m.View())
	require.Contains(t, out, "Claude Code")
	require.Contains(t, out, "Qwen Code")
	require.NotContains(t, out, "Grok Build")
	require.NotContains(t, out, "OpenAI Codex")

	// Nothing installed: the wizard refuses and points at Onboarding.
	none := pickerModel(t)
	none, _ = executeCommand(none, "new")
	require.Equal(t, FocusBoard, none.Focus)
	require.Equal(t, "wizard: no agent is installed (run rex onboard, or see Settings → Onboarding)", none.Err)
}

// With every agent installed the provider step lists Qwen Code, Cursor CLI
// and Grok Build after the others, in registry order; one that is not
// installed moves to Settings → Onboarding like claude or codex would.
func TestWizard_OffersExtraAgentsWhenInstalled(t *testing.T) {
	all := []string{"claude", "codex", "antigravity", "ollama", "qwen", "cursor", "grok"}
	m := pickerModel(t, all...)
	m, _ = executeCommand(m, "new")
	require.Equal(t, FocusWizard, m.Focus)
	require.Equal(t, all, toolIDs(m.Wizard.Tools))
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

func TestWizard_ModelStepMarksDetectedModels(t *testing.T) {
	m := pickerModel(t, "claude", "qwen")
	m, _ = executeCommand(m, "new")
	m = sendKey(m, key("j")) // qwen
	require.Equal(t, "qwen", m.Wizard.selectedTool().ID)
	m = sendKey(m, key("enter"))
	require.Equal(t, wizModel, m.Wizard.Step)
	out := ansi.Strip(m.View())
	require.Regexp(t, `qwen3-coder-plus\s+detected`, out)
	require.Regexp(t, `qwen3-max\s+detected`, out)
	require.NotRegexp(t, `Default model\s+detected`, out, "a built-in model is not marked")
}

// An open wizard picks up ToolsChanged without losing its place.
func TestWizard_ToolsChangedUpdatesInPlace(t *testing.T) {
	m := pickerModel(t, "claude", "qwen")
	m, _ = executeCommand(m, "new")
	m = sendKey(m, key("j"))
	m = sendKey(m, key("enter"))
	m = sendKey(m, key("j")) // the second qwen model
	require.Equal(t, "qwen3-max", m.Wizard.selectedModel().ID)

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

	require.Equal(t, []string{"claude", "qwen", "grok"}, toolIDs(m.Wizard.Tools))
	require.Equal(t, wizModel, m.Wizard.Step, "still on the model step")
	require.Equal(t, "qwen", m.Wizard.selectedTool().ID, "the cursor stays on the same tool")
	require.Equal(t, "qwen3-max", m.Wizard.selectedModel().ID, "and the same model, found by id")
	require.Contains(t, ansi.Strip(m.View()), "qwen3-new")
}

func TestWizard_LongModelListScrollsToTheCursor(t *testing.T) {
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
	m = sendKey(m, key("j"))
	m = sendKey(m, key("enter"))
	for range 30 {
		m = sendKey(m, key("j"))
	}
	want := m.Wizard.selectedModel().Name
	require.Contains(t, ansi.Strip(m.View()), want, "the selected model stays on screen")
	require.Contains(t, ansi.Strip(m.View()), "↑ more")
}

func TestSnapshotAndEvents_CarryTheToolList(t *testing.T) {
	m := goldenBoard(t, goldenSize{120, 40}, false)
	require.False(t, m.toolsKnown)
	snap := protocol.Snapshot{Tools: daemonTools(t, "claude")}
	data, err := json.Marshal(snap)
	require.NoError(t, err)
	m = m.applyEvent(protocol.Envelope{Type: protocol.EventSnapshot, Data: data})
	require.True(t, m.toolsKnown)
	require.Len(t, m.DaemonTools, len(snap.Tools))

	// A snapshot without tools (an older daemon, after a reconnect) is unknown again.
	data, err = json.Marshal(protocol.Snapshot{})
	require.NoError(t, err)
	m = m.applyEvent(protocol.Envelope{Type: protocol.EventSnapshot, Data: data})
	require.False(t, m.toolsKnown)
	require.Nil(t, m.DaemonTools)
}

func TestSettings_OnboardingListsMissingAgents(t *testing.T) {
	m := pickerModel(t, "claude", "qwen")
	m, _ = openSettings(m)
	require.Equal(t, []string{"claude", "qwen"}, toolIDs(m.Settings.Tools), "the pickers offer installed tools only")
	var ids []string
	for _, r := range m.Settings.Onboarding {
		ids = append(ids, r.Tool.ID)
	}
	require.Equal(t, []string{"codex", "antigravity", "ollama", "cursor", "grok"}, ids)

	list := settingsList(m.Settings)
	last := list[len(list)-1]
	require.Equal(t, settings.SectionOnboarding, last.Section)
	require.True(t, last.ReadOnly)

	m.Settings.CursorID = onboardPrefix + "codex"
	out := ansi.Strip(m.View())
	require.Contains(t, out, "Onboarding")
	require.Contains(t, out, "OpenAI Codex")
	require.Contains(t, out, "not on PATH")
	require.Contains(t, out, "command codex")
	require.Contains(t, squash(out), squash("see https://github.com/openai/codex"), "the install hint")

	// A tool with no hint of its own gets the generic one.
	require.Equal(t, "install cursor-agent and put it on your PATH", onboardRow{Tool: registry.Tool{ID: "cursor", Command: []string{"cursor-agent"}}}.hint())

	// The rows are not stored settings: no key changes anything or panics.
	before := m.Store.String("default_spawn_tool")
	for _, k := range []string{"enter", " ", "right", "left", "r", "h", "l"} {
		m = sendKey(m, key(k))
	}
	require.Equal(t, before, m.Store.String("default_spawn_tool"))
	require.Equal(t, FocusSettings, m.Focus)

	// With everything installed the section is absent.
	all := pickerModel(t, "claude", "codex", "antigravity", "ollama", "qwen", "cursor", "grok")
	all, _ = openSettings(all)
	require.Empty(t, all.Settings.Onboarding)
	for _, s := range settingsList(all.Settings) {
		require.NotEqual(t, settings.SectionOnboarding, s.Section)
	}
}

func TestSettings_OnboardingIsSearchable(t *testing.T) {
	m := pickerModel(t, "claude")
	m, _ = openSettings(m)
	m.Settings.Search = "grok"
	var labels []string
	for _, s := range settingsList(m.Settings) {
		labels = append(labels, s.Label)
	}
	require.Contains(t, labels, "Grok Build")
}

func TestSettings_SpawnModelPickerUsesTheMergedList(t *testing.T) {
	m := pickerModel(t, "claude", "qwen")
	m, _ = openSettings(m)
	require.NoError(t, m.Store.Set("default_spawn_tool", "qwen"))
	require.NoError(t, m.Store.Set("default_spawn_model", "qwen3-coder-plus"))
	opts, ok := dynamicOptions(m, "default_spawn_model")
	require.True(t, ok)
	require.Equal(t, []string{"qwen3-coder-plus", "qwen3-max", "default"}, opts)

	m.Settings.CursorID = "default_spawn_model"
	out := ansi.Strip(m.View())
	require.Contains(t, out, "detected", "the detail pane says the current model was reported by the tool")
	m = sendKey(m, key("right"))
	m = sendKey(m, key("right"))
	require.Equal(t, "default", m.Store.String("default_spawn_model"))
	m.Settings.CursorID = "default_spawn_model"
	require.NotContains(t, ansi.Strip(m.View()), "detected", "a built-in model is not marked")
}

// Quick-spawn only offers tools the daemon can start: a default tool that is
// not installed falls back to the first installed one.
func TestSettings_SpawnToolPickerOffersInstalledOnly(t *testing.T) {
	m := pickerModel(t, "claude", "qwen")
	m, _ = openSettings(m)
	opts, _ := dynamicOptions(m, "default_spawn_tool")
	require.Equal(t, []string{"claude", "qwen"}, opts)
}

func TestSetupWizard_ExtraAgentsNeedPathAndTheSwitch(t *testing.T) {
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	cfg := filepath.Join(t.TempDir(), "config.yaml")

	m := NewSetupWizardModel(cfg, "")
	for _, id := range registry.ExtraAgentIDs {
		require.NotContains(t, toolIDs(m.tools), id, "%s: not on PATH", id)
	}

	stub := func(name string) {
		file, body := name, "#!/bin/sh\nexit 0\n"
		if runtime.GOOS == "windows" { // found through PATHEXT
			file, body = name+".bat", "@echo off\r\nexit 0\r\n"
		}
		require.NoError(t, os.WriteFile(filepath.Join(bin, file), []byte(body), 0o755)) //nolint:gosec // a test stub must be executable
	}
	stub("qwen")
	m = NewSetupWizardModel(cfg, "")
	require.Contains(t, toolIDs(m.tools), "qwen")
	require.NotContains(t, toolIDs(m.tools), "grok")

	require.NoError(t, os.WriteFile(cfg, []byte("extra_agents: false\n"), 0o600))
	m = NewSetupWizardModel(cfg, "")
	require.NotContains(t, toolIDs(m.tools), "qwen", "the switch is off")
}
