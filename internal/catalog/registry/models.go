package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Limits on a model-list command.
const (
	// MaxModelsOutput caps the bytes read from a model-list command. A command
	// that prints more is treated as broken, not truncated.
	MaxModelsOutput = 64 << 10
	// maxDetectedModels caps how many ids one tool contributes.
	maxDetectedModels = 200
	// waitDelay bounds how long Wait lingers on pipes a grandchild keeps open
	// after the command was killed.
	waitDelay = 500 * time.Millisecond
)

// ErrNoModels means a model-list command ran but yielded no valid model id
// (an auth error, a usage text, garbage).
var ErrNoModels = errors.New("no models in output")

// modelIDRe is what a detected id must match before it can reach argv.
var modelIDRe = regexp.MustCompile(`^[A-Za-z0-9._:/@\[\]-]{1,128}$`)

// ValidModelID reports whether id is safe to use as a model id: 1-128 of
// letters, digits and . _ : / @ [ ] -, so no space, quote, shell
// metacharacter or leading option marker can arrive from a hostile source.
// An id starting with "-" is refused too, so it can never read as a flag.
func ValidModelID(id string) bool {
	return modelIDRe.MatchString(id) && id[0] != '-'
}

// ParseModelIDs extracts the model ids from the output of mc. Invalid ids and
// duplicates are dropped; the tool's order is kept. Auth errors and other
// text that holds no valid id parse to nothing.
func ParseModelIDs(mc ModelsCmd, out []byte) ([]string, error) {
	var raw []string
	switch mc.Parse {
	case ParseJSON:
		var doc any
		if err := json.Unmarshal(out, &doc); err != nil {
			return nil, fmt.Errorf("parse json: %w", err)
		}
		raw = jsonPathStrings(doc, strings.Split(mc.JSONPath, "."))
	default:
		var re *regexp.Regexp
		if mc.IDRegex != "" {
			re = regexp.MustCompile(mc.IDRegex) // validated at load
		}
		lines := strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
		if mc.SkipHeader > 0 {
			if mc.SkipHeader >= len(lines) {
				lines = nil
			} else {
				lines = lines[mc.SkipHeader:]
			}
		}
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if re == nil {
				raw = append(raw, line)
			} else if m := re.FindStringSubmatch(line); len(m) > 1 {
				raw = append(raw, strings.TrimSpace(m[1]))
			}
		}
	}
	seen := map[string]bool{}
	var ids []string
	for _, id := range raw {
		if !ValidModelID(id) || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
		if len(ids) == maxDetectedModels {
			break
		}
	}
	if len(ids) == 0 {
		return nil, ErrNoModels
	}
	return ids, nil
}

// jsonPathStrings walks doc along JSONPath-lite segments ("models[]", "id")
// and returns the string leaves.
func jsonPathStrings(doc any, segs []string) []string {
	if len(segs) == 0 {
		if s, ok := doc.(string); ok {
			return []string{s}
		}
		return nil
	}
	seg := segs[0]
	isArray := strings.HasSuffix(seg, "[]")
	key := strings.TrimSuffix(seg, "[]")
	if key != "" {
		obj, ok := doc.(map[string]any)
		if !ok {
			return nil
		}
		doc = obj[key]
	}
	if !isArray {
		return jsonPathStrings(doc, segs[1:])
	}
	arr, ok := doc.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, el := range arr {
		out = append(out, jsonPathStrings(el, segs[1:])...)
	}
	return out
}

// runModelsCmd runs the tool's model-list command and returns its stdout.
// The command is Command plus ModelsCmd.Args, fixed in the registry. It gets
// no stdin, the daemon's environment, a timeout, and at most MaxModelsOutput
// bytes of stdout (stderr is discarded). A non-zero exit is an error.
func runModelsCmd(ctx context.Context, tool Tool) ([]byte, error) {
	mc := tool.ModelsCmd
	if mc == nil {
		return nil, errors.New("tool has no models_cmd")
	}
	if len(tool.Command) == 0 {
		return nil, errors.New("tool has no command")
	}
	path, err := exec.LookPath(tool.Command[0])
	if err != nil {
		return nil, fmt.Errorf("binary not found: %w", err)
	}
	timeout := time.Duration(mc.TimeoutMs) * time.Millisecond
	if mc.TimeoutMs == 0 {
		timeout = DefaultModelsTimeoutMs * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	argv := append(append([]string{}, tool.Command[1:]...), mc.Args...)
	cmd := exec.CommandContext(ctx, path, argv...) //nolint:gosec // the command is fixed in the registry
	cmd.Stdin = nil                                // /dev/null
	cmd.WaitDelay = waitDelay
	out := &cappedBuffer{limit: MaxModelsOutput, cancel: cancel}
	cmd.Stdout = out
	if err := cmd.Run(); err != nil {
		switch {
		case out.over:
			return nil, fmt.Errorf("output over %d bytes", MaxModelsOutput)
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			return nil, fmt.Errorf("timed out after %s", timeout)
		}
		return nil, fmt.Errorf("run: %w", err)
	}
	if out.over {
		return nil, fmt.Errorf("output over %d bytes", MaxModelsOutput)
	}
	return out.buf.Bytes(), nil
}

// cappedBuffer collects up to limit bytes; one more byte marks it over and
// cancels the command.
type cappedBuffer struct {
	buf    bytes.Buffer
	limit  int
	over   bool
	cancel context.CancelFunc
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if c.over {
		return 0, errors.New("output over limit")
	}
	if c.buf.Len()+len(p) > c.limit {
		c.over = true
		c.cancel()
		return 0, errors.New("output over limit")
	}
	return c.buf.Write(p)
}

// DetectModels runs the tool's models_cmd and returns the models it lists:
// each id validated, Name the id, Args from model_arg_template, Effort from
// model_effort. An error (missing binary, non-zero exit, timeout, oversized
// output, or output with no valid id such as an auth error) means the caller
// should use the static models.
func DetectModels(ctx context.Context, tool Tool) ([]Model, error) {
	if len(tool.ModelArgTemplate) == 0 {
		return nil, errors.New("tool has no model_arg_template")
	}
	out, err := runModelsCmd(ctx, tool)
	if err != nil {
		return nil, err
	}
	ids, err := ParseModelIDs(*tool.ModelsCmd, out)
	if err != nil {
		return nil, err
	}
	return modelsFromIDs(tool, ids), nil
}

func modelsFromIDs(tool Tool, ids []string) []Model {
	models := make([]Model, len(ids))
	for i, id := range ids {
		models[i] = Model{ID: id, Name: id, Args: tool.ModelArgsFor(id), Effort: tool.ModelEffort, Detected: true}
	}
	return models
}

// MergeModels returns the model list the user sees: the detected models
// first, then the static ones the tool did not list. A detected id that is
// also static keeps its static definition (name, args, effort) and is flagged
// Detected. With nothing detected it is the static list.
func MergeModels(static, detected []Model) []Model {
	if len(detected) == 0 {
		return static
	}
	byID := make(map[string]Model, len(static))
	for _, m := range static {
		byID[m.ID] = m
	}
	out := make([]Model, 0, len(detected)+len(static))
	listed := make(map[string]bool, len(detected))
	for _, d := range detected {
		if listed[d.ID] {
			continue
		}
		listed[d.ID] = true
		if s, ok := byID[d.ID]; ok {
			s.Detected = true
			out = append(out, s)
			continue
		}
		out = append(out, d)
	}
	for _, s := range static {
		if !listed[s.ID] {
			out = append(out, s)
		}
	}
	return out
}

// WithModels returns a copy of the registry in which each tool in detected
// has its models merged (MergeModels). The receiver is not changed. Tool ids
// the registry does not have are ignored.
func (r *Registry) WithModels(detected map[string][]Model) *Registry {
	out := &Registry{Tools: make([]Tool, len(r.Tools)), Warnings: r.Warnings}
	copy(out.Tools, r.Tools)
	for i, t := range out.Tools {
		if d := detected[t.ID]; len(d) > 0 {
			out.Tools[i].Models = MergeModels(t.Models, d)
		}
	}
	return out
}

// ExtraAgentIDs are the tools added by the extra_agents feature.
var ExtraAgentIDs = []string{"qwen", "cursor", "grok"}

// WithFeatures applies the extra_agents and model_autodetect switches to the
// registry, for any process that holds its own copy (the daemon, and the TUI
// wizard and Settings pickers). extraAgents false drops qwen, cursor and grok.
// modelAutodetect true merges the models the daemon cached in cachePath; it
// runs no command (only the daemon does) and an old entry is still shown.
// With it false the cache is not read. The receiver is not changed.
func (r *Registry) WithFeatures(extraAgents, modelAutodetect bool, cachePath string, now time.Time) *Registry {
	out := r
	if !extraAgents {
		out = out.Without(ExtraAgentIDs...)
	}
	if modelAutodetect {
		out = out.WithModels(CachedModels(out.Tools, cachePath, now, true))
	}
	return out
}

// Without returns a copy of the registry without the tools in ids. A user
// tools.yaml overlay for one of them goes with it.
func (r *Registry) Without(ids ...string) *Registry {
	out := &Registry{Warnings: r.Warnings}
	for _, t := range r.Tools {
		skip := false
		for _, id := range ids {
			if t.ID == id {
				skip = true
				break
			}
		}
		if !skip {
			out.Tools = append(out.Tools, t)
		}
	}
	return out
}
