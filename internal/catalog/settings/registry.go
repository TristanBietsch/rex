package settings

import "path/filepath"

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
		Help: "Click/wheel on board + popup. To select text hold Option (iTerm2) / Shift (Ghostty, kitty), or turn off.",
	},
	{
		ID: "max_concurrent_sessions", Label: "Max concurrent sessions", Section: SectionBehavior,
		Type: TypeInt, Default: 16,
		Min: 1, Max: 64,
		Help: "Daemon's PTY cap; takes effect on next daemon start.",
	},
	{
		ID: "retire_after_hours", Label: "Clear marked-done after (h)", Section: SectionBehavior,
		Type: TypeInt, Default: 168,
		Min: 1, Max: 8760,
		Help: "Hours a session marked done (c) lingers before rex kills it and deletes its transcript. Typing into it cancels; a working session waits until it settles.",
	},
	{
		ID: "attach_scrollback_lines", Label: "Attach scrollback lines", Section: SectionBehavior,
		Type: TypeInt, Default: 5000,
		Min: 0, Max: 50000,
		Help: "Lines the attach popup keeps after they scroll off a primary-screen agent (ctrl+pgup / wheel to view). 0 disables. Capped at 8 MiB per popup; applies on next attach.",
	},
	{
		ID: "screensaver_enabled", Label: "Screensaver", Section: SectionBehavior,
		Type: TypeBool, Default: true,
		Help: "When the board gets no key or click for a while, show a small ∴ rex with the counts. Any key or click wakes it and does nothing else.",
	},
	{
		ID: "screensaver_after_minutes", Label: "Screensaver after (min)", Section: SectionBehavior,
		Type: TypeInt, Default: 5,
		Min: 1, Max: 120,
		Help: "Minutes without a key or click on the board before the screensaver shows. A change applies within a minute.",
	},
	{
		ID: "screensaver_keep_awake", Label: "Screensaver keep awake", Section: SectionBehavior,
		Type: TypeBool, Default: true,
		Help: "macOS: keep the display from sleeping while the screensaver shows (caffeinate -d). Does nothing on other systems.",
	},
	{
		ID: "board_hidden_sections", Label: "Hidden board sections", Section: SectionBehavior,
		Type: TypeString, Default: "",
		Help: "Board sections hidden at startup, comma-separated ids: needs, working, done (for example: done). Unknown ids are ignored. zh, z1-z9 and :filter change it for the current run; :bg keeps the choice for the next start.",
	},
	{
		ID: "skip_permissions", Label: "Skip permission prompts", Section: SectionBehavior,
		Type: TypeBool, Default: false,
		Help: "Start Claude Code and Antigravity with --dangerously-skip-permissions. Agents then edit files and run commands without asking you first. Applies to the next session.",
	},

	// Features: one on/off switch per optional feature, read with FeatureOn.
	// Off means inert: no goroutine, subprocess, network, file write or argv
	// addition, keys unbound, commands refuse. Rows are added by the step
	// that adds the feature; each must be a bool or enum with help text.

	// Spawn
	{
		ID: "default_spawn_tool", Label: "Quick-spawn tool", Section: SectionSpawn,
		Type: TypeString, Default: "claude",
		Help: "Tool used when pressing 'i'. Cycle with +/-; changing it resets model and effort.",
	},
	{
		ID: "default_spawn_model", Label: "Quick-spawn model", Section: SectionSpawn,
		Type: TypeString, Default: "opus",
		Help: "Model used when pressing 'i'. Cycles through the selected tool's models.",
	},
	{
		ID: "default_spawn_effort", Label: "Quick-spawn effort", Section: SectionSpawn,
		Type: TypeString, Default: "max",
		Help: "Effort used when pressing 'i'. Cycles through the model's effort options.",
	},
	{
		ID: "auto_trust_dirs", Label: "Auto-trust folders", Section: SectionSpawn,
		Type: TypeString, Default: "",
		Normalize: normalizeAutoTrustDirs,
		Help: "Sessions started in these folders (or below them) skip the agent's \"trust this folder\" question: rex starts the agent with the folder trusted, or answers the question. " +
			"Separate folders with " + string(filepath.ListSeparator) + "; ~/ is your home. Empty: always ask. " +
			"Only list folders whose code you trust: accepting lets a repo's own agent settings apply.",
	},

	// AI summary
	{
		ID: "summary_enabled", Label: "Summary enabled", Section: SectionSummary,
		Type: TypeBool, Default: true,
		Help: "Generate AI activity descriptions via local model.",
	},
	{
		ID: "summary_model", Label: "Summary model", Section: SectionSummary,
		Type: TypeString, Default: "gemma2:2b",
		Options: []string{"gemma2:2b", "llama3.2:3b", "llama3.2:1b", "gemma3:1b", "qwen2.5:3b", "phi3:mini"},
		Help:    "Model used to summarize sessions.",
	},
	{
		ID: "desc_animation", Label: "Description animation", Section: SectionSummary,
		Type: TypeEnum, Default: "typewriter",
		Options: []string{"typewriter", "decode", "wipe", "off"},
		Help:    "Animation effect when the AI description changes.",
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
		Options: []string{"factorio", "evangelion", "bell", "chiptune", "off"},
		Help:    "Synthesized tone catalog.",
	},
	{
		ID: "master_volume", Label: "Master volume", Section: SectionAudio,
		Type: TypeFloat, Default: 0.80,
		Min: 0.0, Max: 1.0,
		Help: "Linear scale applied to every event PCM at playback.",
	},
	{
		ID: "notify_on_state_change", Label: "State-change sounds", Section: SectionAudio,
		Type: TypeBool, Default: true,
		Help: "Chime when an agent needs input (working → needs input) or resumes (needs input → working). Done keeps its own sound.",
	},
	{
		ID: "boot_voice", Label: "Boot voice", Section: SectionAudio,
		Type: TypeBool, Default: true,
		Help: "Spoken \"rex\" announcement after the startup tone (needs sound enabled).",
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
