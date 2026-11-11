package registry

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Registry is the merged tool set.
type Registry struct {
	Tools []Tool
	// Warnings are non-fatal problems found while loading, such as a partial
	// overlay of a retired built-in tool that was dropped. Callers log them.
	Warnings []string
}

// retiredToolIDs are tools that used to be built in. A tools.yaml overlay for
// one of these that no longer has a built-in base is a partial entry (just
// the fields a user tweaked), which cannot stand alone. It is dropped with a
// warning instead of failing the whole load, so an upgrade never leaves the
// daemon unable to start.
var retiredToolIDs = map[string]bool{
	"codex": true, "gemini": true, "ollama": true,
	"grok": true, "deepseek": true, "kimi": true,
}

// Find returns the tool with id, plus whether it was found.
func (r *Registry) Find(id string) (Tool, bool) {
	for _, t := range r.Tools {
		if t.ID == id {
			return t, true
		}
	}
	return Tool{}, false
}

// FindModel returns a model within a tool.
func (r *Registry) FindModel(toolID, modelID string) (Tool, Model, bool) {
	t, ok := r.Find(toolID)
	if !ok {
		return Tool{}, Model{}, false
	}
	for _, m := range t.Models {
		if m.ID == modelID {
			return t, m, true
		}
	}
	return t, Model{}, false
}

// Load reads the built-in registry and merges userPath (if non-empty and exists).
func Load(userPath string) (*Registry, error) {
	var built File
	if err := yaml.Unmarshal(BuiltinBytes(), &built); err != nil {
		return nil, fmt.Errorf("parse builtin registry: %w", err)
	}
	return loadWith(built.Tools, userPath)
}

// loadWith is Load over an explicit built-in tool list.
func loadWith(builtin []Tool, userPath string) (*Registry, error) {
	if err := validate(builtin); err != nil {
		return nil, fmt.Errorf("validate builtin registry: %w", err)
	}

	if userPath == "" {
		return &Registry{Tools: builtin}, nil
	}
	raw, err := os.ReadFile(userPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Registry{Tools: builtin}, nil
		}
		return nil, fmt.Errorf("read user registry %s: %w", userPath, err)
	}
	var user File
	if err := yaml.Unmarshal(raw, &user); err != nil {
		return nil, fmt.Errorf("parse user registry %s: %w", userPath, err)
	}

	merged, warnings := merge(builtin, user.Tools)
	if err := validate(merged); err != nil {
		return nil, fmt.Errorf("validate merged registry: %w", err)
	}
	return &Registry{Tools: merged, Warnings: warnings}, nil
}

// merge applies over to base. An overlay for a retired tool id with no base
// entry that fails validateTool is dropped and reported in warnings; any
// other invalid tool is left for validate to reject.
func merge(base, over []Tool) (tools []Tool, warnings []string) {
	// Index base by id.
	idx := make(map[string]int, len(base))
	out := make([]Tool, len(base))
	copy(out, base)
	for i, t := range out {
		idx[t.ID] = i
	}
	for _, u := range over {
		i, ok := idx[u.ID]
		if !ok {
			if retiredToolIDs[u.ID] {
				if err := validateTool(u); err != nil {
					warnings = append(warnings, fmt.Sprintf(
						"tool %s is no longer built in; define it fully in tools.yaml (docs/registry.md): %v", u.ID, err))
					continue
				}
			}
			out = append(out, u)
			idx[u.ID] = len(out) - 1
			continue
		}
		out[i] = mergeOne(out[i], u)
	}
	return out, warnings
}

func mergeOne(base, over Tool) Tool {
	merged := base
	if over.Name != "" {
		merged.Name = over.Name
	}
	if over.Category != "" {
		merged.Category = over.Category
	}
	if len(over.Command) > 0 {
		merged.Command = over.Command
	}
	if over.CWDStrategy != "" {
		merged.CWDStrategy = over.CWDStrategy
	}
	if over.Detect.Kind != "" {
		merged.Detect = over.Detect
	}
	if over.Icon != "" {
		merged.Icon = over.Icon
	}
	if over.Color != "" {
		merged.Color = over.Color
	}
	if over.TrustPrompt != nil {
		merged.TrustPrompt = over.TrustPrompt
	}
	if len(over.TrustArgs) > 0 {
		merged.TrustArgs = over.TrustArgs
	}
	if len(over.SkipPermissionsArgs) > 0 {
		merged.SkipPermissionsArgs = over.SkipPermissionsArgs
	}
	// Models: extend by id rather than replace.
	if len(over.Models) > 0 {
		mi := make(map[string]int, len(merged.Models))
		for i, m := range merged.Models {
			mi[m.ID] = i
		}
		for _, m := range over.Models {
			if i, ok := mi[m.ID]; ok {
				merged.Models[i] = m
			} else {
				merged.Models = append(merged.Models, m)
				mi[m.ID] = len(merged.Models) - 1
			}
		}
	}
	return merged
}

func validate(tools []Tool) error {
	if len(tools) == 0 {
		return fmt.Errorf("no tools in registry")
	}
	seen := make(map[string]struct{}, len(tools))
	for _, t := range tools {
		if t.ID == "" {
			return fmt.Errorf("tool with empty id")
		}
		if _, dup := seen[t.ID]; dup {
			return fmt.Errorf("duplicate tool id %q", t.ID)
		}
		seen[t.ID] = struct{}{}
		if err := validateTool(t); err != nil {
			return err
		}
	}
	return nil
}

// validateTool checks one tool on its own (everything but id uniqueness).
func validateTool(t Tool) error {
	if t.ID == "" {
		return fmt.Errorf("tool with empty id")
	}
	if len(t.Models) == 0 {
		return fmt.Errorf("tool %q has no models", t.ID)
	}
	switch t.Detect.Kind {
	case "hooks":
		if t.Detect.Format == "" {
			return fmt.Errorf("tool %q: hooks detect needs format", t.ID)
		}
	case "heuristic":
		if t.Detect.PromptRegex == "" || t.Detect.IdleMs <= 0 {
			return fmt.Errorf("tool %q: heuristic detect needs prompt_regex and idle_ms", t.ID)
		}
		if t.Detect.DoneRegex != "" {
			if _, err := regexp.Compile("(?m)" + t.Detect.DoneRegex); err != nil {
				return fmt.Errorf("tool %q: done_regex compile failed: %w", t.ID, err)
			}
		}
		if t.Detect.BusyRegex != "" {
			if _, err := regexp.Compile("(?m)" + t.Detect.BusyRegex); err != nil {
				return fmt.Errorf("tool %q: busy_regex compile failed: %w", t.ID, err)
			}
		}
	default:
		return fmt.Errorf("tool %q: unknown detect.kind %q", t.ID, t.Detect.Kind)
	}
	if len(t.TrustArgs) > 0 && !slices.ContainsFunc(t.TrustArgs, func(a string) bool {
		return strings.Contains(a, TrustArgsCWDList)
	}) {
		return fmt.Errorf("tool %q: trust_args must name the folder with %s", t.ID, TrustArgsCWDList)
	}
	if slices.Contains(t.SkipPermissionsArgs, "") {
		return fmt.Errorf("tool %q: skip_permissions_args must not hold an empty argument", t.ID)
	}
	if tp := t.TrustPrompt; tp != nil {
		if tp.Shown == "" || tp.Selected == "" || tp.Move == "" || tp.Confirm == "" {
			return fmt.Errorf("tool %q: trust_prompt needs shown, selected, move and confirm", t.ID)
		}
		for _, re := range []string{tp.Shown, tp.Selected} {
			if _, err := regexp.Compile(re); err != nil {
				return fmt.Errorf("tool %q: trust_prompt regex compile failed: %w", t.ID, err)
			}
		}
	}
	return nil
}
