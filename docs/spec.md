# rex

A terminal kanban for agent sessions. One unified board across Claude Code, Codex, Gemini, Ollama, and anything else with a CLI. *Needs input · Working · Completed* at a glance; press `enter` to drop into any session.

Rex is the open-source, multi-agent take on Anthropic's [Agent view in Claude Code](https://claude.com/blog/agent-view-in-claude-code): instead of scattered terminal tabs and tmux grids, you get a single Bubble Tea TUI backed by a small daemon that owns the PTYs.

## Design surface

| Doc | Contents |
| --- | --- |
| [stack.md](stack.md) | Libraries and engineering constraints (NASA Power of Ten, Unix philosophy) |
| [architecture.md](architecture.md) | Two-process model, IPC over UDS, persistence layout, adapter interface |
| [protocol.md](protocol.md) | JSONL message vocabulary between `rex` and `rex-daemon` |
| [registry.md](registry.md) | Tool and model registry schema; recipes for adding new models and new tools |
| [tui.md](tui.md) | Screen anatomy, keybinds, mouse, wizard, state machine |
| [design.md](design.md) | Palette, typography, icons, animations, audio |
| [cli.md](cli.md) | CLI commands and flags |
| [settings.md](settings.md) | Settings registry, TUI settings page, Lua config layer |
| [slash.md](slash.md) | `/` slash command palette; Lua-extensible registry |
| [mockup.html](mockup.html) | Static HTML mockup of every screen |

## v0 scope

1. `rex` TUI client and `rex-daemon` over Unix domain socket
2. Built-in tool registry: claude, codex, gemini, ollama; user-defined entries merge on top
3. Adapters: `ClaudeStructured` and `HeuristicCLI`
4. Three-section board, vim keybinds, mouse support, filter chips, incremental search
5. 5-step adaptive new-agent wizard (provider → model → effort → name → confirm), Neovim-style `:` command mode at the bottom, and `:bg` detach with state-restoring reattach via `rex`
6. Enter-modal centered overlay, `z` zoom, `space` peek, inline reply
7. Animations: braille spinner on working rows, 200 ms section slide, counter tick, 120 ms modal scale-in, row pulse
8. Synthesized audio via `oto`, **Factorio-inspired**: seven events (`startup`, `create`, `done`, `delete`, `nav`, `open`, `close`) with sharp-attack/exponential-decay envelopes so they read as mechanical clicks; ANSI BEL fallback
9. Per-session persistence: `meta.json` plus rolling `transcript.log` on disk
10. `~/.config/rex/tools.yaml` merges over the built-in registry — adding a new model is a one-entry YAML edit
11. Settings page (`S` / `:settings`) backed by a single Go registry — appearance, audio, behavior, onboarding model whitelist, advanced; configurable via TUI, `rex config`, YAML, and optional `~/.config/rex/init.lua` (Lua via `gopher-lua`)
12. `/` slash command palette — extensible user-action surface with a small built-in set (`/find`, `/new`, `/help`, `/attach`, `/reply`) and Lua-registerable additions

## Non-goals for v0

- Live PTY survival across daemon restart
- Multi-pane or split layouts (only board ↔ modal)
- Looping or scheduled jobs
- Plugin-process adapters
- Remote daemons over TCP or SSH
- Markdown rendering inside the session modal (glamour is in the stack but not used in v0)

## Open questions

Tracked here so they get resolved in the plan phase rather than the spec phase:

- Exact transcript rotation policy (16 MB cap as written; revisit time-based vs size-based)
- `archive` semantics: hide-only fourth state vs move to soft-trash directory
- Whether `rex agents export <id>` (transcript-to-stdout) belongs in v0 or v1

## Engineering principles

Restated from `stack.md`:

- **NASA Power of Ten.** Bounded loops, no unbounded recursion, every return checked, ~60 LOC ceiling per function, two assertions per function on average, smallest possible data lifetimes, strict lint config.
- **Unix philosophy.** One process per job, text streams on the wire, debuggable with `socat`, `jq`, `tail`, `lsof`.
