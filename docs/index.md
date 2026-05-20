# Documentation index

Rex is a terminal kanban for parallel AI coding-agent sessions. Two binaries: `rex` (client) and `rex-daemon` (supervisor). They communicate over a Unix domain socket.

Go source wins when docs disagree with code.

## By audience

| Audience | Start here |
|----------|------------|
| First-time user | [README](../README.md), then [quickstart.md](quickstart.md) |
| Operator | [cli.md](cli.md), [tui.md](tui.md), [daemon.md](daemon.md), [howto/](howto/) |
| Integrator | [protocol.md](protocol.md) |
| Contributor | [STRUCTURE.md](STRUCTURE.md), [architecture.md](architecture.md) |
| AI agent | [agents/](agents/) |

## Reference

| Document | Purpose |
|----------|---------|
| [cli.md](cli.md) | `rex` commands, flags, exit codes, selectors |
| [tui.md](tui.md) | Board keys, focus modes, attach |
| [slash.md](slash.md) | `:` command palette |
| [settings.md](settings.md) | `config.yaml` keys |
| [registry.md](registry.md) | `tools.yaml` schema and merge rules |
| [protocol.md](protocol.md) | JSONL wire format, intents, events |
| [daemon.md](daemon.md) | `rex-daemon` flags and operations |
| [paths.md](paths.md) | Files, directories, environment variables |
| [glossary.md](glossary.md) | Terms |

## Guides

| Document | Purpose |
|----------|---------|
| [quickstart.md](quickstart.md) | Install to first working session |
| [howto/add-a-tool.md](howto/add-a-tool.md) | Add a tool to the registry |
| [howto/lua-hooks.md](howto/lua-hooks.md) | Lua event hooks |
| [howto/headless-ci.md](howto/headless-ci.md) | Script sessions without the TUI |
| [howto/shell-integration.md](howto/shell-integration.md) | PATH, completion, prompt |
| [howto/customize-appearance.md](howto/customize-appearance.md) | Colors, density, sounds |

## Architecture and layout

| Document | Purpose |
|----------|---------|
| [architecture.md](architecture.md) | Processes, flows, decisions, invariants |
| [STRUCTURE.md](STRUCTURE.md) | Repository layout; where to add code |
| [agents/](agents/) | Agent-oriented module map |

## Undocumented behavior

See [UNDOCUMENTED.md](UNDOCUMENTED.md) for protocol stubs, unused flags, and open design questions.
