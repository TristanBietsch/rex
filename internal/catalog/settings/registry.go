package settings

import (
	"path/filepath"

	"github.com/tristanbietsch/rex/internal/catalog/theme"
)

// normalizeThemeName is the Normalize func of theme. It checks the name's
// shape only: whether a theme of that name exists is decided when it is
// applied (an unknown name falls back to default), so a theme file may be
// created after the setting.
func normalizeThemeName(v string) (string, error) {
	if err := theme.ValidName(v); err != nil {
		return "", err
	}
	return v, nil
}

// Registry is the canonical list of every Rex setting.
//
// Adding a new setting is one struct literal here — the TUI page, the CLI, and
// the persistence layer all pick it up automatically.
var Registry = []Setting{
	// Appearance
	{
		ID: "theme", Label: "Theme", Section: SectionAppearance, Apply: ApplyLive,
		Type: TypeString, Default: "default", Aliases: []string{"color_scheme"},
		Options:   []string{"default", "classic", "noir", "paper", "high-contrast", "ansi", "dracula", "tokyo_night", "pantone_safe", "auto"},
		Normalize: normalizeThemeName,
		Help:      "Change the theme: a built-in (classic is the previous look; ansi uses your terminal's own colors; dracula, tokyo_night and pantone_safe ship as theme files; auto is default on a dark terminal, paper on a light one) or a theme file in ~/.config/rex/themes. An unknown name uses default.",
	},
	{
		ID: "spinner", Label: "Spinner type", Section: SectionAppearance, Apply: ApplyLive,
		Type: TypeEnum, Default: "braille",
		Options: []string{"braille", "ascii_line", "moon", "pulse", "blocks"},
		Help:    "Change the spinner type for loading animations.",
	},
	{
		ID: "waiting_spinner", Label: "Waiting icon", Section: SectionAppearance, Apply: ApplyLive,
		Type: TypeEnum, Default: "clock",
		Options: []string{"clock", "hourglass", "sand", "orbit", "pendulum", "pulse"},
		Help:    "Change the marker of a session waiting on a tool (Waiting section): a clock hand, an hourglass turning over, sand piling up, an orbit, a pendulum or a slow pulse. It steps once a second; reduce_motion holds it still.",
	},
	{
		ID: "row_density", Label: "Row density", Section: SectionAppearance, Apply: ApplyLive,
		Type: TypeEnum, Default: "normal",
		Options: []string{"compact", "normal", "roomy"},
		Help:    "Change the row density.",
	},
	{
		ID: "prompt_glyph", Label: "Prompt glyph", Section: SectionAppearance, Apply: ApplyLive,
		Type: TypeString, Default: "λ",
		Options: []string{"λ", "›", "❯", "▸", ">", "∷", "∴", "⛬", "⍜", "%"},
		Free:    true, Normalize: normalizePromptGlyph,
		Help: "One character that prefixes the bottom prompt: pick a preset with the arrows, or press enter and type your own.",
	},
	{
		ID: "reduce_motion", Label: "Reduce motion", Section: SectionAppearance, Apply: ApplyLive,
		Type: TypeBool, Default: false,
		Help: "Disable all animations.",
	},
	{
		ID: "blinking_enabled", Label: "Done blink", Section: SectionAppearance, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "Disable the blinking of completed rows.",
	},
	{
		ID: "show_help_bar", Label: "Show help bar", Section: SectionAppearance, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "When off, the bottom key hints are hidden. Errors and notices still show.",
	},
	{
		ID: "header_style", Label: "Header style", Section: SectionAppearance, Apply: ApplyLive,
		Type: TypeEnum, Default: "verbose",
		Options: []string{"verbose", "glyphs", "numbers"},
		Help:    "Change the header style for the aggregate counts at the top.",
	},
	{
		ID: "window_border", Label: "Window border", Section: SectionAppearance, Apply: ApplyLive,
		Type: TypeBool, Default: false,
		Help: "Draw a thick colored frame around the whole screen. It gives way on small terminals, the splash and the screensaver. :border changes it for one run.",
	},
	{
		ID: "window_border_color", Label: "Border color", Section: SectionAppearance, Apply: ApplyLive,
		Type: TypeString, Default: "random",
		Options: append([]string{"random"}, theme.EdgeIDs()...),
		Free:    true, Normalize: normalizeBorderColor,
		Help: "random picks a color per launch folder, so a project keeps its color. Or pick a name, or press enter and type #rrggbb.",
	},
	{
		ID: "column_layout", Label: "Column layout", Section: SectionAppearance, Apply: ApplyLive,
		Type: TypeEnum, Default: "fixed",
		Options: []string{"fixed", "fit"},
		Help:    "fixed: board columns have set widths that change only at breakpoints. fit: columns size to their content.",
	},
	{
		ID: "mobile_layout", Label: "Mobile layout", Section: SectionAppearance, Apply: ApplyLive,
		Type: TypeEnum, Default: "auto",
		Options: []string{"auto", "always", "off"},
		Help:    "Two-line rows and a compact header for narrow terminals. auto: 60 columns or fewer. always / off force it.",
	},

	// Behavior
	{
		ID: "keybind_preset", Label: "Keybind preset", Section: SectionBehavior, Apply: ApplyLive, Hidden: true,
		Type: TypeEnum, Default: "default",
		Options: []string{"default"},
		Help:    "Only one preset exists today, so the settings page hides this row. Still readable and settable with rex config.",
	},
	{
		ID: "mouse_enabled", Label: "Mouse enabled", Section: SectionBehavior, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "Click/wheel on board + popup. To select text hold Option (iTerm2) / Shift (Ghostty, kitty), or turn off.",
	},
	{
		ID: "max_concurrent_sessions", Label: "Max concurrent sessions", Section: SectionBehavior, Apply: ApplyFromTUI,
		Type: TypeInt, Default: 16,
		Min: 1, Max: 64,
		Help: "Cap on live PTY sessions. Changing it in the TUI sets the running daemon at once; the daemon never reads it from config.yaml, so a fresh daemon starts at its -max-concurrent-sessions flag (default 16).",
	},
	{
		ID: "retire_after_hours", Label: "Clear marked-done after (h)", Section: SectionBehavior, Apply: ApplyLive,
		Type: TypeInt, Default: 168,
		Min: 1, Max: 8760,
		Help: "Hours a session marked done (c) lingers before rex kills it and deletes its transcript. Typing into it cancels; a working session waits until it settles.",
	},
	{
		ID: "attach_scrollback_lines", Label: "Attach scrollback lines", Section: SectionBehavior, Apply: ApplyNextAttach,
		Type: TypeInt, Default: 5000,
		Min: 0, Max: 50000,
		Help: "Lines the attach popup keeps after they scroll off a primary-screen agent (ctrl+pgup / wheel to view). 0 disables. Capped at 8 MiB per popup; applies on next attach.",
	},
	{
		ID: "screensaver_enabled", Label: "Screensaver", Section: SectionBehavior, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "When rex gets no key or click for a while, show a small ∴ rex with the counts. Any key or click wakes it and does nothing else.",
	},
	{
		ID: "screensaver_after_minutes", Label: "Screensaver after (min)", Section: SectionBehavior, Apply: ApplyLive,
		Type: TypeInt, Default: 5,
		Min: 1, Max: 120,
		Help: "Minutes without a key or click before the screensaver shows. A change applies within a minute.",
	},
	{
		ID: "screensaver_counts", Label: "Screensaver counts", Section: SectionBehavior, Apply: ApplyLive,
		Type: TypeEnum, Default: "horizontal",
		Options: []string{"horizontal", "vertical", "off"},
		Help:    "How the screensaver shows the counts: horizontal on one line, vertical one per line under the logo (back to one line when the terminal is too short), or off.",
	},
	{
		ID: "screensaver_clock", Label: "Screensaver clock", Section: SectionBehavior, Apply: ApplyLive,
		Type: TypeEnum, Default: "off",
		Options: []string{"off", "24h", "12h"},
		Help:    "Show the time under the screensaver's logo, as 14:05 (24h) or 2:05 PM (12h). It updates on the minute.",
	},
	{
		ID: "screensaver_drift", Label: "Screensaver drift", Section: SectionBehavior, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "Move the screensaver a little once a minute so no image stays in one place for hours. Off keeps it centered; reduce_motion also keeps it centered.",
	},
	{
		ID: "screensaver_keep_awake", Label: "Screensaver keep awake", Section: SectionBehavior, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "macOS: keep the display from sleeping while the screensaver shows (caffeinate -d). Does nothing on other systems.",
	},
	{
		ID: "board_hidden_sections", Label: "Hidden board sections", Section: SectionBehavior, Apply: ApplyLive,
		Type: TypeString, Default: "", ListSep: ",",
		Help: "Board sections hidden, comma-separated ids: needs, waiting, working, done (for example: done). Unknown ids are ignored. Applies at once and is the starting set; zh, z1-z9 and :filter change it for the current run; :bg keeps the choice for the next start.",
	},
	{
		ID: "skip_permissions", Label: "Skip permission prompts", Section: SectionBehavior, Apply: ApplyNextSpawn,
		Type: TypeBool, Default: false,
		Help: "Start Claude Code and Antigravity with --dangerously-skip-permissions, but only in trusted folders (under auto_trust_dirs, or trusted for one session with ctrl+t or rex new -trust). Agents then edit files and run commands without asking you first. Applies to the next session.",
	},

	// Features: one on/off switch per optional feature, read with FeatureOn.
	// Off means inert: no goroutine, subprocess, network, file write or argv
	// addition, keys unbound, commands refuse. Rows are added by the step
	// that adds the feature; each must be a bool or enum with help text.
	{
		ID: "board_filter", Label: "Board filters", Section: SectionFeatures, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "Tool filter chips, :filter, section hiding (zh, z1-z9) and board_hidden_sections. Off: no chip row, the t key and the z chords do nothing, :filter refuses, and hidden sections and the saved tool filter are ignored.",
	},
	{
		ID: "search", Label: "Board search", Section: SectionFeatures, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "Search the board with / and :search. Off: / does nothing, :search refuses, and a saved or stale query is ignored.",
	},
	{
		ID: "undo_complete", Label: "Undo done (uc)", Section: SectionFeatures, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "The uc key and :uc / :resume, which take a session out of Done. Off: uc does nothing and the commands refuse; :undone still unmarks a session marked done.",
	},
	{
		ID: "resume", Label: "Resume ids", Section: SectionFeatures, Apply: ApplyNextSpawn,
		Type: TypeBool, Default: true,
		Help: "Track each session's agent conversation id so an exited session can be resumed: the resume id column, :rid and the Resume intent (used by uc / :resume). Off: no id is assigned or captured, the column and :rid go away, and Resume is refused. Read at each spawn.",
	},
	{
		ID: "timestamps", Label: "Start and end times", Section: SectionFeatures, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "Show when each session started and ended: the start→end column in the detailed board view and STARTED / ENDED in rex ls -wide. Off: no column and no such columns; the end time is still recorded.",
	},
	{
		ID: "cli_agent_auto", Label: "Agent mode for Claude Code", Section: SectionFeatures, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "Switch rex's CLI to agent mode (one line per record, no color, no prompts) when CLAUDECODE=1 and stdout is not a terminal. Off: only --agent, -o agent and REX_AGENT=1 do. REX_AGENT=0 always turns it off for one run.",
	},
	{
		ID: "summary_preview", Label: "Summary preview line", Section: SectionFeatures, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "Show the selected session's full AI summary and its age in a line under the board, while any session has a summary. Off: no line, the board uses the row.",
	},
	{
		ID: "summary_schedule", Label: "Scheduled summaries", Section: SectionFeatures, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "Rewrite each running session's AI description on a clock: every 5 minutes for the first 25 minutes, then every 30 minutes, with a more detailed progress line (what is done so far and what is happening now). A state change still refreshes at once; screen activity no longer does. Off: refresh when the screen goes quiet and on state changes, as before. Applies within seconds with live_reload on.",
	},
	{
		ID: "spawn_skills", Label: "Skills per session", Section: SectionFeatures, Apply: ApplyNextSpawn,
		Type: TypeBool, Default: true,
		Help: "Load skills from ~/.config/rex/skills into one session: the skills step in the new-session wizard and rex new -skill, which start Claude Code with --plugin-dir. Off: the step is hidden, -skill refuses, and no --plugin-dir is added (a resumed session loads none). Read at each spawn.",
	},

	{
		ID: "waiting_section", Label: "Waiting section", Section: SectionFeatures, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "A Waiting section below Ready for working sessions blocked on a shell command, subagent or web call for 3 s or more (Claude Code), with its own marker (◷ by default; Waiting icon picks another) and count. Off: they stay in Working, the section, its count and its key go away, and keys 1-3 jump to Ready / Working / Done again. The daemon still records the wait.",
	},

	{
		ID: "turn_snapshots", Label: "Turn snapshots and undo", Section: SectionFeatures, Apply: ApplyLive,
		Type: TypeBool, Default: false,
		Help: "Snapshot a session's folder at the start and end of every agent turn into a private store under ~/.local/share/rex/snapshots, so rex undo can restore it. Only folders inside a git repo are captured, and that repo is never touched. The store holds file contents, including untracked secrets such as .env unless they are gitignored. Off: no snapshot, no git call, and rex undo refuses.",
	},

	{
		ID: "loops", Label: "Loop workflows", Section: SectionFeatures, Apply: ApplyLive,
		Type: TypeBool, Default: false,
		Help: "Run an agent session in a repeat loop (rex loop): the daemon re-sends your prompt each time the agent finishes a turn, until a check command you wrote exits 0 or a cap is reached. Loop files are read only from ~/.config/rex/loops, and their check commands run on your machine. A permission or question prompt pauses the loop; rex never answers it. Off: no loop runs, no loop file is read, and rex loop refuses.",
	},
	{
		ID: "usage", Label: "Usage tracking", Section: SectionFeatures, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "Track agent usage: a ledger of tokens and cost per session (~/.local/share/rex/usage.jsonl) for rex usage, and the quota Claude Code reports through its status line. Off: no status-line tap is added to Claude, no ledger is written, nothing is polled, and rex usage refuses.",
	},
	{
		ID: "usage_statusline_tap", Label: "Claude status-line tap", Section: SectionFeatures, Apply: ApplyNextSpawn,
		Type: TypeEnum, Default: "auto", Options: []string{"auto", "on", "off"},
		Help: "How rex reads Claude Code's plan usage: it adds a status line that passes your own through unchanged. auto: only when you already have a status line (Claude hides its footer hints once one is set, and rex never changes that on its own). on: always. off: never; Claude rows use rex's own estimate. Needs usage on. Read at each spawn.",
	},
	{
		ID: "usage_meter", Label: "Usage meter in the header", Section: SectionFeatures, Apply: ApplyLive,
		Type: TypeEnum, Default: "compact", Options: []string{"off", "compact", "full"},
		Help: "The right side of the header's top row: the worst quota window of each agent that has a live session, as a 5-cell bar and a percent (compact), or every window with its reset time (full). It drops before the counts when the row is short, and a click opens :usage. Needs usage on. off: no meter.",
	},
	{
		ID: "extra_agents", Label: "Extra agents", Section: SectionFeatures, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "Qwen Code (qwen), Cursor CLI (cursor-agent) and Grok Build (grok). Their default modes can edit files or run commands without asking; check each agent's own safety settings. The new-session wizard offers each one only while its binary is on PATH; Settings → Onboarding lists the missing ones with an install hint. Off: the three never show in the wizard or the pickers, and the daemon refuses to spawn them (rex new, quick-spawn) with an off message. A change applies within a few seconds.",
	},
	{
		ID: "model_autodetect", Label: "Model auto-detect", Section: SectionFeatures, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "Ask each installed agent for its own model list (for example qwen --list-models, grok models, cursor-agent models; a few seconds, no network call of rex's own) and cache it in ~/.local/state/rex/models-cache.json for 24 hours. Refreshed at daemon start and by rex reload. Off: no command is run and only the built-in model lists show; the cache is not read or written.",
	},
	{
		ID: "live_reload", Label: "Live settings reload", Section: SectionFeatures, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "The daemon and the TUI notice edits to config.yaml within a few seconds and apply the settings that can change live (see docs/settings.md); the TUI also picks up edits to theme files. Off: no polling; rex reload, :reload and SIGHUP still reload by hand.",
	},
	{
		ID: "session_export", Label: "Session export", Section: SectionFeatures, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "rex export and :export, which write one session as a readable json, yaml, md or txt document (the transcript replayed to clean text, never raw bytes). Off: both refuse. Exports can hold whatever the agent printed, secrets included.",
	},
	{
		ID: "fullscreen_pages", Label: "Full-screen settings and help", Section: SectionFeatures, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "Settings and help open as full-screen pages with a section list on the left, like the board. Off: they open as the older floating overlays, and the page keys (tab between panes, 1-9, [ and ], q) do nothing.",
	},
	{
		ID: "workspaces", Label: "Workspaces", Section: SectionFeatures, Apply: ApplyLive,
		Type: TypeBool, Default: false,
		Help: "Mark a project folder as a workspace with rex ws init: a small .rex/ folder holds an id, a name and a color so sessions in that tree can be told apart. Off: rex never looks for or writes a .rex/ folder, and rex ws refuses.",
	},
	{
		ID: "audit_trail", Label: "Audit trail", Section: SectionFeatures, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "Write one line per finished session (who, which tool, state, times and the first line of the prompt) to ~/.local/share/rex/audit.jsonl, or to <workspace>/.rex/audit.jsonl for a workspace session, for rex audit. Off: no audit file is written and rex audit refuses.",
	},
	{
		ID: "audit_full_prompt", Label: "Audit: full prompt", Section: SectionFeatures, Apply: ApplyLive,
		Type: TypeBool, Default: false,
		Help: "Also store each session's whole first prompt in the audit trail, not only its first line. Prompts can hold secrets; the file is private (0600) but a workspace audit.jsonl may be synced or committed. Needs audit_trail on. Applies to sessions started afterwards.",
	},

	{
		ID: "lua_config", Label: "Lua config", Section: SectionFeatures, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "Set settings and themes in ~/.config/rex/config.lua (rex config edit -lua). Rows set there are locked in the settings page and rex config set refuses them. This switch is read from config.yaml only. Off: config.lua and ~/.config/rex/lua are never read or run, and rows are unlocked.",
	},

	// Spawn
	{
		ID: "default_spawn_tool", Label: "Quick-spawn tool", Section: SectionSpawn, Apply: ApplyNextSpawn,
		Type: TypeString, Default: "claude",
		Help: "Tool used when pressing 'i'. Cycle with +/-; changing it resets model and effort.",
	},
	{
		ID: "default_spawn_model", Label: "Quick-spawn model", Section: SectionSpawn, Apply: ApplyNextSpawn,
		Type: TypeString, Default: "opus",
		Help: "Model used when pressing 'i'. Cycles through the selected tool's models.",
	},
	{
		ID: "default_spawn_effort", Label: "Quick-spawn effort", Section: SectionSpawn, Apply: ApplyNextSpawn,
		Type: TypeString, Default: "max",
		Help: "Effort used when pressing 'i'. Cycles through the model's effort options.",
	},
	{
		ID: "auto_trust_dirs", Label: "Auto-trust folders", Section: SectionSpawn, Apply: ApplyNextSpawn,
		Type: TypeString, Default: "",
		Free: true, Normalize: normalizeAutoTrustDirs, ListSep: string(filepath.ListSeparator),
		Help: "Sessions started in these folders (or below them) skip the agent's \"trust this folder\" question: rex starts the agent with the folder trusted, or answers the question. " +
			"Separate folders with " + string(filepath.ListSeparator) + "; ~/ is your home. Empty: always ask. " +
			"Only list folders whose code you trust: accepting lets a repo's own agent settings apply.",
	},

	// AI summary
	{
		ID: "summary_enabled", Label: "Summary enabled", Section: SectionSummary, Apply: ApplyDaemonRestart,
		Type: TypeBool, Default: true,
		Help: "Generate AI activity descriptions via local model.",
	},
	{
		ID: "summary_model", Label: "Summary model", Section: SectionSummary, Apply: ApplyDaemonRestart,
		Type: TypeString, Default: "gemma2:2b",
		Options: []string{"gemma2:2b", "llama3.2:3b", "llama3.2:1b", "gemma3:1b", "qwen2.5:3b", "phi3:mini"},
		Help:    "Model used to summarize sessions.",
	},
	{
		ID: "summary_detail", Label: "Summary detail", Section: SectionSummary, Apply: ApplyLive,
		Type: TypeEnum, Default: "medium",
		Options: []string{"short", "medium", "long"},
		Help:    "How much the AI description says: short about 8 words, medium about 15, long about 28. Applies within seconds.",
	},
	{
		ID: "desc_animation", Label: "Description animation", Section: SectionSummary, Apply: ApplyLive,
		Type: TypeEnum, Default: "typewriter",
		Options: []string{"typewriter", "decode", "wipe", "off"},
		Help:    "Animation effect when the AI description changes.",
	},
	{
		ID: "overview_enabled", Label: "Overview", Section: SectionSummary, Apply: ApplyLive,
		Type: TypeBool, Default: false,
		Help: "Header line + :overview: what is ready for you, what to review, what finished. Uses the summary model (one call at most every 30 s while rex is open). Applies within seconds.",
	},
	{
		ID: "attach_banner", Label: "Attach banner", Section: SectionSummary, Apply: ApplyNextAttach,
		Type: TypeEnum, Default: "marquee",
		Options: []string{"marquee", "static", "off"},
		Help:    "Row above the agent in the attach popup: what it is doing, where, and (with overview) what the rest are doing. reduce_motion makes marquee static. Hidden below 25 rows. Read when a popup opens and on resize.",
	},

	// Audio
	{
		ID: "sound_enabled", Label: "Sound enabled", Section: SectionAudio, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "Master on/off; suppresses BEL fallback too.",
	},
	{
		ID: "soundset", Label: "Soundset", Section: SectionAudio, Apply: ApplyLive,
		Type: TypeEnum, Default: "factorio",
		Options: []string{"factorio", "evangelion", "bell", "chiptune", "off"},
		Help:    "Synthesized tone catalog.",
	},
	{
		ID: "master_volume", Label: "Master volume", Section: SectionAudio, Apply: ApplyLive,
		Type: TypeInt, Default: VolumeDefault,
		Min: 0, Max: VolumeMax, MaxText: "max", Migrate: migrateVolume,
		Help: "Volume of every sound, 0 (silent) to 50 (max). An older config's 0.0–1.0 value is read as that share of 50.",
	},
	{
		ID: "notify_on_state_change", Label: "State-change sounds", Section: SectionAudio, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "Chime when an agent needs input (working → needs input) or resumes (needs input → working). Done keeps its own sound.",
	},
	{
		ID: "boot_voice", Label: "Boot voice", Section: SectionAudio, Apply: ApplyLive,
		Type: TypeBool, Default: true,
		Help: "Spoken \"rex\" announcement after the startup tone (needs sound enabled).",
	},
	// Advanced
	{
		ID: "lua_config_path", Label: "Lua config", Section: SectionAdvanced, Apply: ApplyDaemonRestart,
		Type: TypeString, Default: "~/.config/rex/init.lua",
		ReadOnly: true,
		Help:     "Path to the optional Lua config the daemon loads at startup.",
	},
	{
		ID: "cli_error_log", Label: "CLI error log", Section: SectionAdvanced, Apply: ApplyLive,
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

// Visible returns the settings the TUI page lists: every setting but the
// Hidden ones. The CLI lists them all.
func Visible() []Setting {
	out := make([]Setting, 0, len(Registry))
	for _, s := range Registry {
		if !s.Hidden {
			out = append(out, s)
		}
	}
	return out
}
