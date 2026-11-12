package registry

import (
	"fmt"
	"regexp"
	"strings"
)

// ModelsCmd is how rex asks a tool for its models. The command is the tool's
// own Command[0] plus Args, fixed in the registry and never built from
// session output.
type ModelsCmd struct {
	Args  []string `yaml:"args"`            // appended to Command[0]
	Parse string   `yaml:"parse,omitempty"` // "lines" (default) | "json"
	// JSONPath picks the ids out of JSON output (parse: json), as
	// JSONPath-lite: dot-separated keys where "key[]" walks an array, for
	// example "models[].id".
	JSONPath string `yaml:"json_path,omitempty"`
	// IDRegex (parse: lines) pulls the id from a line: capture group 1. A line
	// that does not match is skipped. Empty: the trimmed line is the id.
	IDRegex string `yaml:"id_regex,omitempty"`
	// SkipHeader drops that many leading lines before matching (parse: lines).
	SkipHeader int `yaml:"skip_header,omitempty"`
	TimeoutMs  int `yaml:"timeout_ms,omitempty"` // 100-30000; 0 = DefaultModelsTimeoutMs
}

// Models list parsers.
const (
	ParseLines = "lines"
	ParseJSON  = "json"
)

// Bounds and defaults for ModelsCmd.
const (
	DefaultModelsTimeoutMs = 5000
	minModelsTimeoutMs     = 100
	maxModelsTimeoutMs     = 30000
	maxSkipHeader          = 20
)

// ModelArgTemplatePlaceholder is the model_arg_template placeholder for a model id.
const ModelArgTemplatePlaceholder = "{model}"

// PromptArgsPlaceholder is the prompt_args placeholder for the task text.
const PromptArgsPlaceholder = "{prompt}"

// PromptPaste says the tool takes its first prompt pasted into the PTY, not
// on the command line (a REPL such as ollama run).
type PromptPaste struct {
	// Plain skips DEC bracketed-paste markers (line-oriented REPLs).
	Plain bool `yaml:"plain,omitempty"`
	// SettleMs is how long output must be quiet before pasting.
	SettleMs int `yaml:"settle_ms,omitempty"`
}

const maxPromptSettleMs = 10000

// PromptArgsFor returns PromptArgs with {prompt} replaced by prompt. The
// prompt stays inside one argv element whatever it holds (nil when unset).
func (t Tool) PromptArgsFor(prompt string) []string {
	if len(t.PromptArgs) == 0 {
		return nil
	}
	out := make([]string, len(t.PromptArgs))
	for i, a := range t.PromptArgs {
		out[i] = strings.ReplaceAll(a, PromptArgsPlaceholder, prompt)
	}
	return out
}

// ModelArgsFor returns ModelArgTemplate with {model} replaced by id (nil when
// the tool has no template). The id must already be validated.
func (t Tool) ModelArgsFor(id string) []string {
	if len(t.ModelArgTemplate) == 0 {
		return nil
	}
	out := make([]string, len(t.ModelArgTemplate))
	for i, a := range t.ModelArgTemplate {
		out[i] = strings.ReplaceAll(a, ModelArgTemplatePlaceholder, id)
	}
	return out
}

var jsonPathSeg = regexp.MustCompile(`^(\[\]|[A-Za-z0-9_-]+(\[\])?)$`)

// validJSONPath reports whether p is well-formed JSONPath-lite.
func validJSONPath(p string) bool {
	for _, seg := range strings.Split(p, ".") {
		if !jsonPathSeg.MatchString(seg) {
			return false
		}
	}
	return true
}

func validateModelsCmd(id string, mc *ModelsCmd) error {
	if mc == nil {
		return nil
	}
	if len(mc.Args) == 0 {
		return fmt.Errorf("tool %q: models_cmd.args must not be empty", id)
	}
	for _, a := range mc.Args {
		if a == "" {
			return fmt.Errorf("tool %q: models_cmd.args must not hold an empty argument", id)
		}
	}
	switch mc.Parse {
	case "", ParseLines:
		if mc.JSONPath != "" {
			return fmt.Errorf("tool %q: models_cmd.json_path only applies to parse: json", id)
		}
		if mc.IDRegex != "" {
			re, err := regexp.Compile(mc.IDRegex)
			if err != nil {
				return fmt.Errorf("tool %q: models_cmd.id_regex compile failed: %w", id, err)
			}
			if re.NumSubexp() < 1 {
				return fmt.Errorf("tool %q: models_cmd.id_regex needs a capture group for the id", id)
			}
		}
	case ParseJSON:
		if mc.JSONPath == "" || !validJSONPath(mc.JSONPath) {
			return fmt.Errorf("tool %q: models_cmd.json_path %q is not valid (example: models[].id)", id, mc.JSONPath)
		}
		if mc.IDRegex != "" || mc.SkipHeader != 0 {
			return fmt.Errorf("tool %q: models_cmd.id_regex only applies to parse: lines", id)
		}
	default:
		return fmt.Errorf("tool %q: models_cmd.parse %q must be lines or json", id, mc.Parse)
	}
	if mc.JSONPath != "" && mc.Parse != ParseJSON {
		return fmt.Errorf("tool %q: models_cmd.json_path only applies to parse: json", id)
	}
	if mc.SkipHeader < 0 || mc.SkipHeader > maxSkipHeader {
		return fmt.Errorf("tool %q: models_cmd.skip_header must be 0-%d", id, maxSkipHeader)
	}
	if mc.TimeoutMs != 0 && (mc.TimeoutMs < minModelsTimeoutMs || mc.TimeoutMs > maxModelsTimeoutMs) {
		return fmt.Errorf("tool %q: models_cmd.timeout_ms must be %d-%d", id, minModelsTimeoutMs, maxModelsTimeoutMs)
	}
	return nil
}

// validateTemplates checks model_arg_template, prompt_args, prompt_paste and
// login_regex.
func validateTemplates(t Tool) error {
	if len(t.ModelArgTemplate) > 0 {
		if containsEmpty(t.ModelArgTemplate) {
			return fmt.Errorf("tool %q: model_arg_template must not hold an empty argument", t.ID)
		}
		if countIn(t.ModelArgTemplate, ModelArgTemplatePlaceholder) == 0 {
			return fmt.Errorf("tool %q: model_arg_template must name the model with %s", t.ID, ModelArgTemplatePlaceholder)
		}
	}
	if len(t.PromptArgs) > 0 {
		if containsEmpty(t.PromptArgs) {
			return fmt.Errorf("tool %q: prompt_args must not hold an empty argument", t.ID)
		}
		if countIn(t.PromptArgs, PromptArgsPlaceholder) != 1 {
			return fmt.Errorf("tool %q: prompt_args must hold %s exactly once", t.ID, PromptArgsPlaceholder)
		}
		if t.PromptPaste != nil {
			return fmt.Errorf("tool %q: prompt_args and prompt_paste are exclusive", t.ID)
		}
	}
	if pp := t.PromptPaste; pp != nil && (pp.SettleMs < 0 || pp.SettleMs > maxPromptSettleMs) {
		return fmt.Errorf("tool %q: prompt_paste.settle_ms must be 0-%d", t.ID, maxPromptSettleMs)
	}
	if t.LoginRegex != "" {
		if _, err := regexp.Compile(t.LoginRegex); err != nil {
			return fmt.Errorf("tool %q: login_regex compile failed: %w", t.ID, err)
		}
	}
	return nil
}

func containsEmpty(args []string) bool {
	for _, a := range args {
		if a == "" {
			return true
		}
	}
	return false
}

// countIn counts the occurrences of sub across all of args.
func countIn(args []string, sub string) int {
	n := 0
	for _, a := range args {
		n += strings.Count(a, sub)
	}
	return n
}
