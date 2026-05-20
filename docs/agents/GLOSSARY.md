# Glossary

Project-specific terms. Alphabetical.

## adapter

Output classifier implementing `internal/daemon/adapter.Adapter`; maps PTY bytes to `protocol.State`. Origin: `internal/daemon/adapter`.

## board

TUI kanban view of sessions grouped by state. Origin: `internal/surface/tui/board.go`, `FocusBoard`.

## daemon

`rex-daemon` process: UDS server, session store, PTY supervisors. Origin: `cmd/rex-daemon`, `internal/daemon/server`.

## description

AI-generated one-line activity summary on a session (`SessionSummary.description`). Origin: `internal/features/summarizer`, `protocol.SessionSummary`.

## fleet

Named label grouping related sessions (`SessionSummary.fleet`). Origin: `protocol`, `IntentSetSessionFleet`, `rex fleet`.

## intent

Client→daemon message (`protocol.KindIntent`). Origin: `internal/wire/protocol/intents.go`.

## registry

Merged tool catalog (`registry.Load`, `builtin.yaml` + `tools.yaml`). Origin: `internal/catalog/registry`.

## selector

CLI/TUI session reference: short id, slug, or `@state` alias. Origin: `internal/surface/cli/selector.go`.

## session

One agent PTY instance with metadata and transcript. Origin: `internal/daemon/state.Session`.

## short_id

Four-character (or extended) display id derived from session UUID. Origin: `internal/runtime/ids`, `SessionSummary.short_id`.

## slug

Human-readable session identifier (kebab-case task name). Origin: `SessionSummary.slug`, wizard.

## snapshot

Full board state returned in `EventSnapshot` after `Hello`. Origin: `protocol.Snapshot`.

## soundset

Named audio catalog (`factorio`, `evangelion`, etc.). Origin: `internal/features/audio`, setting `soundset`.

## store

In-memory `state.Store` of sessions; authoritative inside daemon. Origin: `internal/daemon/state`.

## summarizer

Ollama-backed worker updating descriptions. Origin: `internal/features/summarizer`.

## tool

Registry entry for an agent CLI (`registry.Tool`, `tool_id`). Origin: `internal/catalog/registry`.

## transcript

Append-only `transcript.log` of raw PTY output per session. Origin: `internal/daemon/state/persist.go`.

## TUI

Terminal UI (`rex` with no args). Origin: `internal/surface/tui`.

## UDS

Unix domain socket carrying JSONL protocol. Default path: see `daemonctl.DefaultSocket`.

## wizard

Interactive new-session flow in TUI (`WizardState`) or `rex setup` (`SetupWizardModel`). Origin: `internal/surface/tui/wizard.go`, `setup_wizard.go`.
