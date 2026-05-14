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
}

// Detect describes how the adapter decides session state.
type Detect struct {
	Kind        string `yaml:"kind"`             // "structured" | "heuristic"
	Format      string `yaml:"format,omitempty"` // when kind=structured
	PromptRegex string `yaml:"prompt_regex,omitempty"`
	IdleMs      int    `yaml:"idle_ms,omitempty"`
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
