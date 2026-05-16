package settings

// Registry is the canonical list of every Rex setting.
//
// Adding a new setting is one struct literal here — the TUI page, the CLI, and
// the persistence layer all pick it up automatically.
var Registry = []Setting{
	// Appearance
	{
		ID: "color_scheme", Label: "Color scheme", Section: SectionAppearance,
		Type: TypeEnum, Default: "default",
		Options: []string{"default", "noir", "paper"},
		Help:    "Change the color scheme.",
	},
	{
		ID: "spinner", Label: "Spinner type", Section: SectionAppearance,
		Type: TypeEnum, Default: "braille",
		Options: []string{"braille", "ascii_line", "moon", "pulse", "blocks"},
		Help:    "Change the spinner type for loading animations.",
	},
	{
		ID: "row_density", Label: "Row density", Section: SectionAppearance,
		Type: TypeEnum, Default: "normal",
		Options: []string{"compact", "normal", "roomy"},
		Help:    "Change the row density.",
	},
	{
		ID: "prompt_glyph", Label: "Prompt glyph", Section: SectionAppearance,
		Type: TypeString, Default: "λ",
		Options: []string{"λ", "›", "❯", "▸", ">", "∷", "∴", "⛬", "⍜", "%"},
		Help:    "Grapheme that prefixes the bottom prompt.",
	},
	{
		ID: "reduce_motion", Label: "Reduce motion", Section: SectionAppearance,
		Type: TypeBool, Default: false,
		Help: "Disable all animations.",
	},
	{
		ID: "blinking_enabled", Label: "Done blink", Section: SectionAppearance,
		Type: TypeBool, Default: true,
		Help: "Disable the blinking of completed rows.",
	},
	{
		ID: "show_help_bar", Label: "Show help bar", Section: SectionAppearance,
		Type: TypeBool, Default: true,
		Help: "When off, the bottom help line is not displayed.",
	},
	{
		ID: "header_style", Label: "Header style", Section: SectionAppearance,
		Type: TypeEnum, Default: "verbose",
		Options: []string{"verbose", "glyphs", "numbers"},
		Help:    "Change the header style for the aggregate counts at the top.",
	},

	// Audio
	{
		ID: "sound_enabled", Label: "Sound enabled", Section: SectionAudio,
		Type: TypeBool, Default: true,
		Help: "Master on/off; suppresses BEL fallback too.",
	},
	{
		ID: "soundset", Label: "Soundset", Section: SectionAudio,
		Type: TypeEnum, Default: "factorio",
		Options: []string{"factorio", "evangelion", "off"},
		Help:    "Synthesized tone catalog.",
	},
	{
		ID: "master_volume", Label: "Master volume", Section: SectionAudio,
		Type: TypeFloat, Default: 0.80,
		Min: 0.0, Max: 1.0,
		Help: "Linear scale applied to every event PCM at playback.",
	},

	// Behavior
	{
		ID: "keybind_preset", Label: "Keybind preset", Section: SectionBehavior,
		Type: TypeEnum, Default: "default",
		Options: []string{"default"},
		Help:    "Future: vim / emacs / nano. Lua can rebind regardless.",
	},
	{
		ID: "mouse_enabled", Label: "Mouse enabled", Section: SectionBehavior,
		Type: TypeBool, Default: true,
		Help: "When off, the TUI ignores mouse events.",
	},
	{
		ID: "max_concurrent_sessions", Label: "Max concurrent sessions", Section: SectionBehavior,
		Type: TypeInt, Default: 16,
		Min: 1, Max: 64,
		Help: "Daemon's PTY cap; takes effect on next daemon start.",
	},

	// AI summary
	{
		ID: "summary_enabled", Label: "Summary enabled", Section: SectionSummary,
		Type: TypeBool, Default: true,
		Help: "Generate AI activity descriptions via local Ollama. When off, the desc column shows the raw last line.",
	},
	{
		ID: "summary_model", Label: "Summary model", Section: SectionSummary,
		Type: TypeString, Default: "gemma2:2b",
		Options: []string{"gemma2:2b", "llama3.2:1b", "phi3:mini", "qwen2.5:1.5b"},
		Help:    "Ollama model used to summarize sessions. Cycles through known small models; any pulled model works via config.yaml.",
	},
	{
		ID: "desc_animation", Label: "Description animation", Section: SectionSummary,
		Type: TypeEnum, Default: "typewriter",
		Options: []string{"typewriter", "decode", "wipe", "off"},
		Help:    "Animation effect when the AI description changes. Reduce motion overrides this to off.",
	},

	// Advanced
	{
		ID: "lua_config_path", Label: "Lua config", Section: SectionAdvanced,
		Type: TypeString, Default: "~/.config/rex/init.lua",
		ReadOnly: true,
		Help:     "Path to the optional Lua config (Plan E).",
	},
}

// Find returns the Setting with id and whether it exists.
func Find(id string) (Setting, bool) {
	for _, s := range Registry {
		if s.ID == id {
			return s, true
		}
	}
	return Setting{}, false
}
