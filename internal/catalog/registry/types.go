// Package registry defines the tool/model registry shape and the merge of built-ins with user YAML.
package registry

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
	// expands to a JSON array of the session folder's paths. Preferred over
	// TrustPrompt when the agent supports it.
	TrustArgs []string `yaml:"trust_args,omitempty"`
	// SkipPermissionsArgs are extra argv that make the agent run without
	// asking for approval (for example --dangerously-skip-permissions). They
	// are added only while the skip_permissions setting is on.
	SkipPermissionsArgs []string `yaml:"skip_permissions_args,omitempty"`
}

// TrustArgsCWDList is the trust_args placeholder for the session folder.
const TrustArgsCWDList = "{cwd_list}"

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
}

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
