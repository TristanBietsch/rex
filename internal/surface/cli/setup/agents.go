package setup

import (
	"os/exec"
	"slices"

	"github.com/tristanbietsch/rex/internal/catalog/registry"
	"github.com/tristanbietsch/rex/internal/catalog/settings"
	"github.com/tristanbietsch/rex/internal/runtime/paths"
	"github.com/tristanbietsch/rex/internal/surface/tui"
)

// agentStatus is one known coding agent as this process sees it: whether
// its binary is on the CLI's PATH, and whether rex offers it at all.
type agentStatus struct {
	ID   string
	Name string
	// Bin is the binary the agent needs on PATH; Path is where it was found.
	Bin  string
	Path string
	// Installed is whether Bin is on PATH.
	Installed bool
	// Off is true for an extra agent (qwen, cursor, grok) while extra_agents
	// is off: rex does not offer it even when installed.
	Off bool
	// Hint is how to install it (the registry's install_hint).
	Hint string
}

// lookPath finds a binary on PATH; tests replace it.
var lookPath = exec.LookPath

// scanAgents lists the known agents (tools.yaml merged over the built-ins,
// as the wizard offers them) with their install state. A tools.yaml that
// does not load falls back to the built-ins, as the board does.
func scanAgents() []agentStatus {
	reg := loadAgentRegistry()
	st, _, _ := settings.LoadDefault()
	extraOn := settings.FeatureOn(st, "extra_agents")
	var out []agentStatus
	for _, t := range tui.KnownAgents(reg) {
		a := agentStatus{
			ID:   t.ID,
			Name: t.Name,
			Bin:  tui.AgentCommand(t),
			Off:  !extraOn && slices.Contains(registry.ExtraAgentIDs, t.ID),
			Hint: tui.AgentInstallHint(t),
		}
		if a.Name == "" {
			a.Name = t.ID
		}
		if p, err := lookPath(a.Bin); err == nil {
			a.Installed, a.Path = true, p
		}
		out = append(out, a)
	}
	return out
}

// loadAgentRegistry is the user's registry, or the built-ins when tools.yaml
// is missing or broken.
func loadAgentRegistry() *registry.Registry {
	if path, err := paths.ToolsFile(); err == nil {
		if reg, err := registry.Load(path); err == nil {
			return reg
		}
	}
	reg, err := registry.Load("")
	if err != nil {
		return &registry.Registry{}
	}
	return reg
}

// usableAgents counts the agents rex offers that are installed.
func usableAgents(agents []agentStatus) int {
	n := 0
	for _, a := range agents {
		if a.Installed && !a.Off {
			n++
		}
	}
	return n
}
