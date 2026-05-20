package registry

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Registry is the merged tool set.
type Registry struct {
	Tools []Tool
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
	if err := validate(built.Tools); err != nil {
		return nil, fmt.Errorf("validate builtin registry: %w", err)
	}

	if userPath == "" {
		return &Registry{Tools: built.Tools}, nil
	}
	raw, err := os.ReadFile(userPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Registry{Tools: built.Tools}, nil
		}
		return nil, fmt.Errorf("read user registry %s: %w", userPath, err)
	}
	var user File
	if err := yaml.Unmarshal(raw, &user); err != nil {
		return nil, fmt.Errorf("parse user registry %s: %w", userPath, err)
	}

	merged := merge(built.Tools, user.Tools)
	if err := validate(merged); err != nil {
		return nil, fmt.Errorf("validate merged registry: %w", err)
	}
	return &Registry{Tools: merged}, nil
}

func merge(base, over []Tool) []Tool {
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
			out = append(out, u)
			idx[u.ID] = len(out) - 1
			continue
		}
		out[i] = mergeOne(out[i], u)
	}
	return out
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
		if len(t.Models) == 0 {
			return fmt.Errorf("tool %q has no models", t.ID)
		}
		switch t.Detect.Kind {
		case "structured":
			if t.Detect.Format == "" {
				return fmt.Errorf("tool %q: structured detect needs format", t.ID)
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
		default:
			return fmt.Errorf("tool %q: unknown detect.kind %q", t.ID, t.Detect.Kind)
		}
	}
	return nil
}
