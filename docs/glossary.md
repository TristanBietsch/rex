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

## long-running badge

Small row column (`▴42m`) on sessions that have been `working` without a break for 10+ minutes; color tiers escalate at 30 min, 2 h, 6 h. Driven by `working_since`.

## mark done

User action (`cc`, `rex done`) that schedules a session to be cleared after `retire_after_hours` without stopping it. Called **retire** on the wire (`Retire` / `retire_at`) to keep it distinct from the `done` state. Typing into the session cancels it. The board moves a marked session to Completed with a `done <time>` badge (`retired_at` on the wire).

## name

User-chosen display label shown instead of the slug on the board. Free text, max 64 characters; never a selector.

## registry

Merged tool catalog: embedded `builtin.yaml` plus optional `~/.config/rex/tools.yaml`.

## selector

Argument that identifies one session: full UUID, exact slug, or hex short id (prefix match on session id).

## session

One agent child process with metadata (`meta.json`) and an append-only transcript (`transcript.log`).

## short_id

Display id derived from the session UUID (typically four hex characters).

## slug

Human-readable session handle (kebab-case) used as a selector. Unique across every session the daemon holds (any state, until removed): `NewSession` and `Rename` to a slug another session uses are rejected with `bad_intent` (`slug "x" already in use by <short_id>`). No whitespace or control characters. Sessions restored from an older daemon may still share a slug; selectors then report ambiguity (use the short id, or rename one). For a free-text label use **name**.

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
