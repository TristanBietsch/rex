// Package registry defines the tool/model registry shape and the merge of built-ins with user YAML.
package registry

import "strings"

// Tool is one entry in the registry.
type Tool struct {
	ID               string   `yaml:"id"`
	Name             string   `yaml:"name"`
	Category         string   `yaml:"category"` // "paid" | "self_hosted"
	Command          []string `yaml:"command"`
	CWDStrategy      string   `yaml:"cwd_strategy,omitempty"` // "inherit" | "session_dir"
	Detect           Detect   `yaml:"detect"`
	Icon             string   `yaml:"icon"`
	Color            string   `yaml:"color"`
	EnabledByDefault *bool    `yaml:"enabled_by_default,omitempty"`
	Models           []Model  `yaml:"models"`
	// TrustPrompt describes the agent's folder-trust dialog so the daemon
	// can accept it for sessions under auto_trust_dirs. nil: no dialog.
	TrustPrompt *TrustPrompt `yaml:"trust_prompt,omitempty"`
	// TrustArgs are extra argv for sessions under auto_trust_dirs that make
	// the agent trust the folder itself, so no dialog shows. {cwd_list}
	// expands to a JSON array of the session folder's paths; a switch that
	// turns the check off for the whole run (qwen) needs no placeholder.
	// Preferred over TrustPrompt when the agent supports it.
	TrustArgs []string `yaml:"trust_args,omitempty"`
	// SkipPermissionsArgs are extra argv that make the agent run without
	// asking for approval (for example --dangerously-skip-permissions). They
	// are added only while the skip_permissions setting is on.
	SkipPermissionsArgs []string `yaml:"skip_permissions_args,omitempty"`
	// SkillArgs are extra argv that load the session's staged skills plugin
	// (for example --plugin-dir {plugin_dir}). {plugin_dir} expands to the
	// plugin folder under the session directory. nil: the tool cannot load
	// skills per session.
	SkillArgs []string `yaml:"skill_args,omitempty"`
	// Resume says how to resume the agent's conversation and how rex learns
	// its id. nil: the tool cannot be resumed.
	Resume *ResumeSpec `yaml:"resume,omitempty"`
	// StatusEnv are NAME=value entries added to the child's environment to
	// turn its status channel on (agy: AGY_CLI_OSC7501=on). Only with
	// detect.status; tools that answer the OSC 7501 query need none.
	StatusEnv []string `yaml:"status_env,omitempty"`
	// ModelsCmd asks the tool for its models; nil: static Models only.
	ModelsCmd *ModelsCmd `yaml:"models_cmd,omitempty"`
	// ModelEffort is the effort spec detected models get (a detected model has
	// no hand-written effort block). nil: detected models have no effort.
	ModelEffort *Effort `yaml:"model_effort,omitempty"`
	// ModelArgTemplate turns a detected model id into argv ({model} is the
	// id), since a detected model has no hand-written args.
	ModelArgTemplate []string `yaml:"model_arg_template,omitempty"`
	// PromptArgs carry the task text on the command line, appended after all
	// other args: {prompt} stays one argv element (claude: ["--", "{prompt}"]).
	// Without PromptArgs and PromptPaste the tool gets no initial prompt.
	PromptArgs []string `yaml:"prompt_args,omitempty"`
	// PromptPaste pastes the task text into the PTY once the TUI settles.
	PromptPaste *PromptPaste `yaml:"prompt_paste,omitempty"`
	// LoginRegex matches the agent's signed-out screen: the session shows
	// needs_input with a "not signed in" notice.
	LoginRegex string `yaml:"login_regex,omitempty"`
}

// TrustArgsCWDList is the trust_args placeholder for the session folder.
const TrustArgsCWDList = "{cwd_list}"

// SkillArgsPluginDir is the skill_args placeholder for the plugin folder.
const SkillArgsPluginDir = "{plugin_dir}"

// SkillArgsFor returns SkillArgs with {plugin_dir} replaced by dir. The
// folder stays one argument even when it holds spaces (nil when unset).
func (t Tool) SkillArgsFor(dir string) []string {
	if len(t.SkillArgs) == 0 {
		return nil
	}
	out := make([]string, len(t.SkillArgs))
	for i, a := range t.SkillArgs {
		out[i] = strings.ReplaceAll(a, SkillArgsPluginDir, dir)
	}
	return out
}

// TrustPrompt is an agent's "do you trust this folder?" dialog: how to tell
// it is on screen and that its accept option is the selected one, and the
// keys that move the selection and confirm it.
type TrustPrompt struct {
	Shown    string `yaml:"shown"`    // regex: the dialog is on screen
	Selected string `yaml:"selected"` // regex: the accept option is selected
	Move     string `yaml:"move"`     // keys that move the selection one step
	Confirm  string `yaml:"confirm"`  // keys that confirm the selection
}

// Detect describes how the adapter decides session state.
type Detect struct {
	Kind        string `yaml:"kind"`             // "hooks" | "heuristic"
	Format      string `yaml:"format,omitempty"` // when kind=hooks (e.g. "claude")
	PromptRegex string `yaml:"prompt_regex,omitempty"`
	DoneRegex   string `yaml:"done_regex,omitempty"` // optional; flips heuristic to StateDone after idle
	// BusyRegex is optional text the agent shows only while it works (e.g.
	// "esc to cancel"). When it appeared after the last prompt match, a quiet
	// stretch shorter than the 15 s stale rule stays working.
	BusyRegex string `yaml:"busy_regex,omitempty"`
	IdleMs    int    `yaml:"idle_ms,omitempty"`
	// Status names a per-session status channel the agent reports its state
	// on, layered over the heuristic (kind=heuristic only). "osc7501" is
	// Antigravity's terminal status sequence; the daemon turns it on through
	// the child's environment. "" means none.
	Status string `yaml:"status,omitempty"`
}

// StatusOSC7501 is the Detect.Status value for OSC 7501 terminal status.
const StatusOSC7501 = "osc7501"

// Model is one variant of a tool.
type Model struct {
	ID         string   `yaml:"id"`
	Name       string   `yaml:"name"`
	Args       []string `yaml:"args,omitempty"`
	ArgsPrompt string   `yaml:"args_prompt,omitempty"` // free-form value asked at launch
	Effort     *Effort  `yaml:"effort,omitempty"`
}

// Effort is an optional reasoning-effort spec.
type Effort struct {
	Options     []string `yaml:"options"`
	Default     string   `yaml:"default"`
	ArgTemplate string   `yaml:"arg_template"` // e.g. "--effort={value}"
}

// File is the on-disk root of a tools.yaml file.
type File struct {
	Tools []Tool `yaml:"tools"`
}
