# Glossary

Project-specific terms. Alphabetical. See [index.md](index.md) for documentation map.

## adapter

Output classifier that maps PTY bytes to a session state (`working`, `needs_input`, `done`). Selected per tool in the registry.

## board

TUI kanban view: sessions grouped into Needs input, Working, and Completed columns.

## daemon

The `rex-daemon` process. Owns the Unix socket, session store, and one PTY supervisor per session.

## description

One-line activity summary on a session row. Optional. Produced by the local summarizer (Ollama) when enabled.

## fleet

Named label on a session. Groups related work. Set with `rex fleet set` or at spawn time.

## intent

Client message sent to the daemon over the socket. JSON envelope with `kind: "Intent"`.

## registry

Merged tool catalog: embedded `builtin.yaml` plus optional `~/.config/rex/tools.yaml`.

## selector

Argument that identifies one session: full UUID, exact slug, or hex short id (prefix match on session id).

## session

One agent child process with metadata (`meta.json`) and an append-only transcript (`transcript.log`).

## short_id

Display id derived from the session UUID (typically four hex characters).

## slug

Human-readable session name (kebab-case). Unique among active sessions.

## snapshot

Full session list sent to a client immediately after `Hello`.

## soundset

Named set of UI sounds (`factorio`, `evangelion`, `bell`, `chiptune`, or `off`).

## store

In-memory session map inside the daemon. Authoritative while the daemon runs.

## summarizer

Background worker that calls Ollama to update session descriptions.

## tool

Registry entry for an agent CLI (`tool_id`, command line, models, state detection).

## transcript

Raw PTY output appended to `transcript.log` under the session directory.

## TUI

Terminal UI started by `rex` with no subcommand.

## UDS

Unix domain socket. Carries newline-delimited JSON. Default path: [paths.md](paths.md).

## wizard

Interactive flow to create a session (TUI) or run first-time setup (`rex setup`).
