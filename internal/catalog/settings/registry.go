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
	{
		ID: "column_layout", Label: "Column layout", Section: SectionAppearance,
		Type: TypeEnum, Default: "fixed",
		Options: []string{"fixed", "fit"},
		Help:    "fixed: board columns have set widths that change only at breakpoints. fit: columns size to their content.",
	},
	{
		ID: "mobile_layout", Label: "Mobile layout", Section: SectionAppearance,
		Type: TypeEnum, Default: "auto",
		Options: []string{"auto", "always", "off"},
		Help:    "Two-line rows and a compact header for narrow terminals. auto: 60 columns or fewer. always / off force it.",
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
		Help: "When rex gets no key or click for a while, show a small ∴ rex with the counts. Any key or click wakes it and does nothing else.",
	},
	{
		ID: "screensaver_after_minutes", Label: "Screensaver after (min)", Section: SectionBehavior,
		Type: TypeInt, Default: 5,
		Min: 1, Max: 120,
		Help: "Minutes without a key or click before the screensaver shows. A change applies within a minute.",
	},
	{
		ID: "screensaver_keep_awake", Label: "Screensaver keep awake", Section: SectionBehavior,
		Type: TypeBool, Default: true,
		Help: "macOS: keep the display from sleeping while the screensaver shows (caffeinate -d). Does nothing on other systems.",
	},
	{
		ID: "board_hidden_sections", Label: "Hidden board sections", Section: SectionBehavior,
		Type: TypeString, Default: "",
		Help: "Board sections hidden at startup, comma-separated ids: needs, waiting, working, done (for example: done). Unknown ids are ignored. zh, z1-z9 and :filter change it for the current run; :bg keeps the choice for the next start.",
	},
	{
		ID: "skip_permissions", Label: "Skip permission prompts", Section: SectionBehavior,
		Type: TypeBool, Default: false,
		Help: "Start Claude Code and Antigravity with --dangerously-skip-permissions, but only in folders under auto_trust_dirs. Agents then edit files and run commands without asking you first. Applies to the next session.",
	},

	// Features: one on/off switch per optional feature, read with FeatureOn.
	// Off means inert: no goroutine, subprocess, network, file write or argv
	// addition, keys unbound, commands refuse. Rows are added by the step
	// that adds the feature; each must be a bool or enum with help text.
	{
		ID: "board_filter", Label: "Board filters", Section: SectionFeatures,
		Type: TypeBool, Default: true,
		Help: "Tool filter chips, :filter, section hiding (zh, z1-z9) and board_hidden_sections. Off: no chip row, the t key and the z chords do nothing, :filter refuses, and hidden sections and the saved tool filter are ignored.",
	},
	{
		ID: "search", Label: "Board search", Section: SectionFeatures,
		Type: TypeBool, Default: true,
		Help: "Search the board with / and :search. Off: / does nothing, :search refuses, and a saved or stale query is ignored.",
	},
	{
		ID: "undo_complete", Label: "Undo done (uc)", Section: SectionFeatures,
		Type: TypeBool, Default: true,
		Help: "The uc key and :uc / :resume, which take a session out of Done. Off: uc does nothing and the commands refuse; :undone still unmarks a session marked done.",
	},
	{
		ID: "resume", Label: "Resume ids", Section: SectionFeatures,
		Type: TypeBool, Default: true,
		Help: "Track each session's agent conversation id so an exited session can be resumed: the resume id column, :rid and the Resume intent (used by uc / :resume). Off: no id is assigned or captured, the column and :rid go away, and Resume is refused. Read at each spawn.",
	},
	{
		ID: "timestamps", Label: "Start and end times", Section: SectionFeatures,
		Type: TypeBool, Default: true,
		Help: "Show when each session started and ended: the start→end column in the detailed board view and STARTED / ENDED in rex ls -wide. Off: no column and no such columns; the end time is still recorded.",
	},
	{
		ID: "cli_agent_auto", Label: "Agent mode for Claude Code", Section: SectionFeatures,
		Type: TypeBool, Default: true,
		Help: "Switch rex's CLI to agent mode (one line per record, no color, no prompts) when CLAUDECODE=1 and stdout is not a terminal. Off: only --agent, -o agent and REX_AGENT=1 do. REX_AGENT=0 always turns it off for one run.",
	},
	{
		ID: "summary_preview", Label: "Summary preview line", Section: SectionFeatures,
		Type: TypeBool, Default: true,
		Help: "Show the selected session's full AI summary and its age in a line under the board, while any session has a summary. Off: no line, the board uses the row.",
	},
	{
		ID: "spawn_skills", Label: "Skills per session", Section: SectionFeatures,
		Type: TypeBool, Default: true,
		Help: "Load skills from ~/.config/rex/skills into one session: the skills step in the new-session wizard and rex new -skill, which start Claude Code with --plugin-dir. Off: the step is hidden, -skill refuses, and no --plugin-dir is added (a resumed session loads none). Read at each spawn.",
	},

	{
		ID: "waiting_section", Label: "Waiting section", Section: SectionFeatures,
		Type: TypeBool, Default: true,
		Help: "A Waiting section below Needs you for working sessions blocked on a shell command, subagent or web call for 3 s or more (Claude Code), with its own ◷ marker and count. Off: they stay in Working, the section, its count and its key go away, and keys 1-3 jump to Needs you / Working / Done again. The daemon still records the wait.",
	},

	{
		ID: "turn_snapshots", Label: "Turn snapshots and undo", Section: SectionFeatures,
		Type: TypeBool, Default: false,
		Help: "Snapshot a session's folder at the start and end of every agent turn into a private store under ~/.local/share/rex/snapshots, so rex undo can restore it. Only folders inside a git repo are captured, and that repo is never touched. The store holds file contents, including untracked secrets such as .env unless they are gitignored. Off: no snapshot, no git call, and rex undo refuses.",
	},

	{
		ID: "loops", Label: "Loop workflows", Section: SectionFeatures,
		Type: TypeBool, Default: false,
		Help: "Run an agent session in a repeat loop (rex loop): the daemon re-sends your prompt each time the agent finishes a turn, until a check command you wrote exits 0 or a cap is reached. Loop files are read only from ~/.config/rex/loops, and their check commands run on your machine. A permission or question prompt pauses the loop; rex never answers it. Off: no loop runs, no loop file is read, and rex loop refuses.",
	},
	{
		ID: "usage", Label: "Usage tracking", Section: SectionFeatures,
		Type: TypeBool, Default: true,
		Help: "Track agent usage: a ledger of tokens and cost per session (~/.local/share/rex/usage.jsonl) for rex usage, and the quota Claude Code reports through its status line. Off: no status-line tap is added to Claude, no ledger is written, nothing is polled, and rex usage refuses.",
	},
	{
		ID: "usage_statusline_tap", Label: "Claude status-line tap", Section: SectionFeatures,
		Type: TypeEnum, Default: "auto", Options: []string{"auto", "on", "off"},
		Help: "How rex reads Claude Code's plan usage: it adds a status line that passes your own through unchanged. auto: only when you already have a status line (Claude hides its footer hints once one is set, and rex never changes that on its own). on: always. off: never; Claude rows use rex's own estimate. Needs usage on. Read at each spawn.",
	},
	{
		ID: "live_reload", Label: "Live settings reload", Section: SectionFeatures,
		Type: TypeBool, Default: true,
		Help: "The daemon notices edits to config.yaml within a few seconds and applies the settings that can change live (see docs/settings.md). Off: no polling; rex reload and SIGHUP still reload by hand.",
	},
	{
		ID: "session_export", Label: "Session export", Section: SectionFeatures,
		Type: TypeBool, Default: true,
		Help: "rex export and :export, which write one session as a readable json, yaml, md or txt document (the transcript replayed to clean text, never raw bytes). Off: both refuse. Exports can hold whatever the agent printed, secrets included.",
	},
	{
		ID: "workspaces", Label: "Workspaces", Section: SectionFeatures,
		Type: TypeBool, Default: false,
		Help: "Mark a project folder as a workspace with rex ws init: a small .rex/ folder holds an id, a name and a color so sessions in that tree can be told apart. Off: rex never looks for or writes a .rex/ folder, and rex ws refuses.",
	},
	{
		ID: "audit_trail", Label: "Audit trail", Section: SectionFeatures,
		Type: TypeBool, Default: true,
		Help: "Write one line per finished session (who, which tool, state, times and the first line of the prompt) to ~/.local/share/rex/audit.jsonl, or to <workspace>/.rex/audit.jsonl for a workspace session, for rex audit. Off: no audit file is written and rex audit refuses.",
	},
	{
		ID: "audit_full_prompt", Label: "Audit: full prompt", Section: SectionFeatures,
		Type: TypeBool, Default: false,
		Help: "Also store each session's whole first prompt in the audit trail, not only its first line. Prompts can hold secrets; the file is private (0600) but a workspace audit.jsonl may be synced or committed. Needs audit_trail on. Applies to sessions started afterwards.",
	},

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
		ID: "summary_detail", Label: "Summary detail", Section: SectionSummary,
		Type: TypeEnum, Default: "medium",
		Options: []string{"short", "medium", "long"},
		Help:    "How much the AI description says: short about 8 words, medium about 15, long about 28. Applies within seconds.",
	},
	{
		ID: "desc_animation", Label: "Description animation", Section: SectionSummary,
		Type: TypeEnum, Default: "typewriter",
		Options: []string{"typewriter", "decode", "wipe", "off"},
		Help:    "Animation effect when the AI description changes.",
	},
	{
		ID: "overview_enabled", Label: "Overview", Section: SectionSummary,
		Type: TypeBool, Default: false,
		Help: "Header line + :overview: what needs you, what is ready for review, what finished. Uses the summary model (one call at most every 30 s while rex is open). Applies within seconds.",
	},
	{
		ID: "attach_banner", Label: "Attach banner", Section: SectionSummary,
		Type: TypeEnum, Default: "marquee",
		Options: []string{"marquee", "static", "off"},
		Help:    "Row above the agent in the attach popup: what it is doing, where, and (with overview) what the rest are doing. reduce_motion makes marquee static. Hidden below 25 rows. Read when a popup opens and on resize.",
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
	{
		ID: "cli_error_log", Label: "CLI error log", Section: SectionAdvanced,
		Type: TypeBool, Default: true,
		Help: "Keep a private record of failed rex commands in ~/.local/state/rex/cli-errors.log: structure only (command, flag names, exit code and kind, error type names), never any message, prompt, value or path. Capped at 1 MiB plus one backup. Read it with rex errors. Env REX_ERROR_LOG=0 also turns it off.",
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
