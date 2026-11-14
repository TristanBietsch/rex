package registry

import (
	"fmt"
	"slices"
	"strings"
)

// SpawnChoice is a tool, model and effort, as stored in default_spawn_*.
type SpawnChoice struct {
	Tool, Model, Effort string
}

// ResolveSpawn applies the tool, model and effort a user asked for (any may
// be empty) on top of cur and checks the result against the registry. An
// unknown tool, a model the tool does not have or an effort the model does
// not offer is an error that lists the valid ids. A new tool or model
// repairs what depends on it: the model falls back to the tool's first, the
// effort to the model's default. changed is false when nothing was asked for.
func (r *Registry) ResolveSpawn(cur SpawnChoice, tool, model, effort string) (out SpawnChoice, changed bool, err error) {
	out = cur
	if tool == "" && model == "" && effort == "" {
		return out, false, nil
	}
	if tool != "" {
		if _, ok := r.Find(tool); !ok {
			return cur, false, fmt.Errorf("unknown tool %q (valid: %s)", tool, strings.Join(r.toolIDs(), ", "))
		}
		if tool != cur.Tool {
			out.Tool, out.Model, out.Effort = tool, "", ""
		}
	}
	t, ok := r.Find(out.Tool)
	if !ok {
		return cur, false, fmt.Errorf("unknown tool %q (valid: %s)", out.Tool, strings.Join(r.toolIDs(), ", "))
	}
	if len(t.Models) == 0 {
		return cur, false, fmt.Errorf("tool %q has no models", t.ID)
	}
	if model != "" {
		out.Model = model
	}
	var m Model
	found := false
	for _, md := range t.Models {
		if md.ID == out.Model {
			m, found = md, true
			break
		}
	}
	if !found {
		if model != "" {
			return cur, false, fmt.Errorf("unknown model %q for tool %s (valid: %s)", model, t.ID, strings.Join(modelIDs(t), ", "))
		}
		m = t.Models[0]
		out.Model, out.Effort = m.ID, ""
	}
	switch {
	case effort != "":
		if m.Effort == nil || !slices.Contains(m.Effort.Options, effort) {
			valid := "none: this model takes no effort"
			if m.Effort != nil {
				valid = strings.Join(m.Effort.Options, ", ")
			}
			return cur, false, fmt.Errorf("unknown effort %q for %s/%s (valid: %s)", effort, t.ID, m.ID, valid)
		}
		out.Effort = effort
	case m.Effort == nil:
		out.Effort = ""
	case !slices.Contains(m.Effort.Options, out.Effort):
		out.Effort = m.Effort.Default
	}
	return out, true, nil
}

// toolIDs lists the tool ids a user may name, without the hidden test tool.
func (r *Registry) toolIDs() []string {
	var ids []string
	for _, t := range r.Tools {
		if t.ID != "echo" {
			ids = append(ids, t.ID)
		}
	}
	return ids
}

func modelIDs(t Tool) []string {
	ids := make([]string, 0, len(t.Models))
	for _, m := range t.Models {
		ids = append(ids, m.ID)
	}
	return ids
}
