package tui

import (
	"log/slog"
	"slices"
	"strings"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/wire/protocol"
)

// Tool pickers (R02-4 TUI part, R02-6): the new-session wizard, the quick-spawn
// settings and Settings → Onboarding offer the tools the daemon can start.
// The tool definitions come from the registry files the TUI loads; whether a
// tool is installed and which models it has (the detected ones merged with
// the static list) come from the daemon, in Snapshot.tools and ToolsChanged.

// onboardPrefix starts the id of a Settings → Onboarding row, which is not a
// stored setting.
const onboardPrefix = "agent:"

// onboardRow is a tool the wizard cannot offer because its binary is not on
// the daemon's PATH.
type onboardRow struct {
	Tool registry.Tool
}

// setting is the row as the settings page lists it: read only, never stored.
func (r onboardRow) setting() settings.Setting {
	return settings.Setting{
		ID:       onboardPrefix + r.Tool.ID,
		Label:    protocol.FlattenDisplayText(r.Tool.Name),
		Section:  settings.SectionOnboarding,
		Type:     settings.TypeString,
		ReadOnly: true,
		Help:     "Not on PATH, so the new-session wizard does not offer it. Install it: " + r.hint(),
	}
}

// command is the binary the tool needs.
func (r onboardRow) command() string {
	if len(r.Tool.Command) == 0 {
		return r.Tool.ID
	}
	return protocol.FlattenDisplayText(r.Tool.Command[0])
}

// hint is how to install the tool: the registry's install_hint, else a
// generic line naming the binary.
func (r onboardRow) hint() string {
	if h := protocol.FlattenDisplayText(r.Tool.InstallHint); h != "" {
		return h
	}
	return "install " + r.command() + " and put it on your PATH"
}

// pickerTools is what the wizard and the quick-spawn pickers offer, and the
// Onboarding rows for the tools left out because they are not installed.
// reg is the registry the TUI loaded. The extra agents (qwen, cursor, grok)
// are dropped while extra_agents is off. With the daemon's tool list known,
// a tool the daemon does not list or reports as not installed is not
// offered, and each tool's models are the daemon's merged list; against a
// daemon that sent none (it predates it) every tool counts as installed.
func pickerTools(m Model, reg *registry.Registry) (offer []registry.Tool, onboarding []onboardRow) {
	if !m.featureOn("extra_agents") {
		reg = reg.Without(registry.ExtraAgentIDs...)
	}
	var installed map[string]bool
	if m.toolsKnown {
		reg, installed = overlayDaemonTools(reg, m.DaemonTools)
	}
	for _, t := range visibleTools(reg.Tools) {
		if installed == nil || installed[t.ID] {
			offer = append(offer, t)
		} else {
			onboarding = append(onboarding, onboardRow{Tool: t})
		}
	}
	return offer, onboarding
}

// overlayDaemonTools applies the daemon's tool list to reg: it returns a copy
// whose tools carry the daemon's models, and which tools the daemon reports
// as installed (a tool it does not list is not).
func overlayDaemonTools(reg *registry.Registry, infos []protocol.ToolInfo) (*registry.Registry, map[string]bool) {
	byID := make(map[string]protocol.ToolInfo, len(infos))
	for _, in := range infos {
		byID[in.ID] = in
	}
	out := &registry.Registry{Tools: slices.Clone(reg.Tools), Warnings: reg.Warnings, Disabled: reg.Disabled}
	installed := make(map[string]bool, len(out.Tools))
	for i, t := range out.Tools {
		in, ok := byID[t.ID]
		installed[t.ID] = ok && in.Installed
		if ok && len(in.Models) > 0 {
			out.Tools[i].Models = modelsFromDaemon(t, in.Models)
		}
	}
	return out, installed
}

// modelsFromDaemon is the tool's model list in the daemon's order. A model
// the registry also lists keeps its definition (args, effort); one the tool
// reported only gets the tool's own model arguments and effort, as the daemon
// builds it.
func modelsFromDaemon(t registry.Tool, infos []protocol.ModelInfo) []registry.Model {
	static := make(map[string]registry.Model, len(t.Models))
	for _, md := range t.Models {
		static[md.ID] = md
	}
	out := make([]registry.Model, 0, len(infos))
	for _, in := range infos {
		md, ok := static[in.ID]
		if !ok {
			name := in.Name
			if name == "" {
				name = in.ID
			}
			md = registry.Model{ID: in.ID, Name: name, Args: t.ModelArgsFor(in.ID), Effort: t.ModelEffort}
		}
		md.Detected = in.Detected
		out = append(out, md)
	}
	return out
}

// adoptDaemonTools records the daemon's tool list and refreshes the open
// wizard and settings page with it.
func adoptDaemonTools(m Model, infos []protocol.ToolInfo) Model {
	m.DaemonTools, m.toolsKnown = infos, true
	return refreshPickers(m)
}

// adoptSnapshotTools takes the tool list of a Snapshot: none (a daemon that
// predates it) means the list is unknown.
func adoptSnapshotTools(m Model, infos []protocol.ToolInfo) Model {
	if len(infos) == 0 {
		m.DaemonTools, m.toolsKnown = nil, false
		return refreshPickers(m)
	}
	return adoptDaemonTools(m, infos)
}

// refreshPickers rebuilds the open wizard's, settings page's and new-agent page's tool lists
// after the daemon's list or a setting changed. The cursor stays on the same
// tool and model by id when they are still offered.
func refreshPickers(m Model) Model {
	if m.Wizard == nil && m.Settings == nil && m.NewAgent == nil {
		return m
	}
	reg, err := loadToolsStrict()
	if err != nil {
		slog.Warn("tui: tool registry could not be reloaded; pickers keep their lists", "err", err)
		return m
	}
	offer, onboarding := pickerTools(m, reg)
	if m.Wizard != nil && len(offer) == 0 {
		// Nothing left to offer (the last agent went away, or extra_agents
		// was turned off): there is nothing to pick.
		m.Wizard = nil
		m.Focus = FocusBoard
		m.Notice = "the new-session wizard closed: no agent is available (see Settings → Onboarding)"
	}
	if m.Wizard != nil {
		w := *m.Wizard
		toolID, modelID, effort := w.selectedTool().ID, w.selectedModel().ID, w.currentEffort()
		w.Tools = offer
		w.ToolIdx = max(slices.IndexFunc(offer, func(t registry.Tool) bool { return t.ID == toolID }), 0)
		t := w.selectedTool()
		if i := slices.IndexFunc(t.Models, func(md registry.Model) bool { return md.ID == modelID }); t.ID == toolID && i >= 0 {
			w.ModelIdx = i
		} else {
			presetModelDefault(&w)
		}
		// The effort stays where the user put it while the model is the same
		// and still offers it.
		presetEffortDefault(&w)
		// The step the user was on may not exist for the new tool or model
		// (no effort block, no skills): step back to one that does.
		for w.Step > wizProvider && !w.stepApplies(w.Step) {
			w.Step--
		}
		if opts := w.selectedModel().Effort; opts != nil && w.selectedModel().ID == modelID {
			if i := slices.Index(opts.Options, effort); i >= 0 {
				w.EffortIdx = i
			}
		}
		m.Wizard = &w
	}
	if m.Settings != nil {
		st := *m.Settings
		st.Tools, st.Onboarding = offer, onboarding
		m.Settings = &st
	}
	if m.NewAgent != nil {
		m = refreshNewAgent(m, offer, onboarding)
	}
	return m
}

// onboardRowOf is the Onboarding row with the settings id id, if any.
func onboardRowOf(st *SettingsState, id string) (onboardRow, bool) {
	if st == nil || !strings.HasPrefix(id, onboardPrefix) {
		return onboardRow{}, false
	}
	for _, r := range st.Onboarding {
		if onboardPrefix+r.Tool.ID == id {
			return r, true
		}
	}
	return onboardRow{}, false
}

// detectedModel reports, for the default_spawn_model row, whether the
// current model is one the tool's own model list reported (ok is false for
// every other row).
func detectedModel(m Model, id string) (detected, ok bool) {
	if id != "default_spawn_model" || m.Settings == nil || len(m.Settings.Tools) == 0 {
		return false, false
	}
	_, model, _ := currentSpawnChoice(m)
	return model.Detected, true
}

// unavailableNotice says a saved default tool is not offered and which one
// replaces it.
func unavailableNotice(tool, other string) string {
	return protocol.FlattenDisplayText(tool) + " isn't available; using " + protocol.FlattenDisplayText(other)
}

// quickChoice is the tool, model and effort a quick-spawn (i) starts.
type quickChoice struct{ Tool, Model, Effort string }

// quickChoiceOf reads the quick-spawn defaults. A saved default_spawn_tool
// that the pickers do not offer (hidden by extra_agents, or not installed)
// is replaced by the first tool that is, with that tool's first model and
// its default effort, and notice says so. With no registry or nothing
// offered the saved values stand and the daemon decides.
func quickChoiceOf(m Model) (c quickChoice, notice string) {
	c.Tool, _ = m.Store.Get("default_spawn_tool").(string)
	c.Model, _ = m.Store.Get("default_spawn_model").(string)
	c.Effort, _ = m.Store.Get("default_spawn_effort").(string)
	reg, err := loadToolsStrict()
	if err != nil {
		slog.Warn("tui: quick-spawn could not check the tool registry", "err", err)
		return c, ""
	}
	offer, _ := pickerTools(m, reg)
	if len(offer) == 0 || slices.ContainsFunc(offer, func(t registry.Tool) bool { return t.ID == c.Tool }) {
		return c, ""
	}
	first := offer[0]
	notice = unavailableNotice(c.Tool, first.ID)
	c.Tool, c.Model, c.Effort = first.ID, "", ""
	if len(first.Models) > 0 {
		md := first.Models[0]
		c.Model = md.ID
		if md.Effort != nil {
			c.Effort = md.Effort.Default
		}
	}
	return c, notice
}
