# Decisions

Non-obvious choices an agent might undo without context.

## Two binaries, UDS protocol

**Chosen:** `rex` (client/TUI) and `rex-daemon` (supervisor) with newline-delimited JSON over Unix socket.

**Rejected:** Single process owning both TUI and PTYs (blocks alt-screen) or TCP/HTTP control plane.

**Why:** Keeps PTY supervision alive when TUI exits; allows CLI scripting and multiple clients.

## Session state from adapters, not agent APIs

**Chosen:** Classify `working` / `needs_input` / `done` from PTY output via regex, or from agent hooks where the CLI has them (Claude Code: hooks injected with `--settings`, state appended to `hooks.log`) (`internal/daemon/adapter`). Claude's `--output-format=stream-json` only works with `--print`, so the interactive TUI can't be parsed as JSON.

**Rejected:** Polling proprietary agent HTTP APIs per vendor.

**Why:** Works for any CLI agent; registry `detect` block selects strategy per tool.

## Persist transcripts as raw bytes

**Chosen:** Append-only `transcript.log` per session; sanitize only for classification/summarizer.

**Rejected:** Structured message log only.

**Why:** Attach/replay and debugging need faithful PTY stream. Readable text (board `last_line`, summarizer input) comes from a per-session vt10x screen in the supervisor, because full-screen TUIs repaint changed cells only and escape-stripping can't recover their text.

## Crash sessions on daemon restart

**Chosen:** Reload `meta.json` but remap `queued`/`working`/`needs_input` → `crashed`.

**Rejected:** Attempt to resume live PTY children after daemon death.

**Why:** OS child processes are gone after daemon exit; state must not imply live agents.

## Settings single registry slice

**Chosen:** `settings.Registry` []Setting drives TUI, CLI, and YAML store.

**Rejected:** Duplicate setting definitions per surface.

**Why:** One edit point when adding a setting (`registry.go` comment).

## Summarizer optional and local

**Chosen:** Ollama HTTP worker in daemon; disable when channel nil or setting off.

**Rejected:** Cloud summary service in core path.

**Why:** Descriptions are enhancement; must not block core kanban.

## Audio degrades silently

**Chosen:** `audio.New` always returns `Player`; device init failure → no-op `Play`.

**Rejected:** Fail TUI startup when no audio device.

**Why:** CI/SSH/headless environments must run board.

## `rex-daemon` version string `v1`

**Chosen:** Constant `version = "v1"` in `cmd/rex-daemon/main.go` for `-version` flag.

**Not:** Go module version or git tag sync.

**Why:** Wire protocol version is `protocol.ProtocolVersion`; daemon banner version is independent (do not conflate when debugging).

## IntentOpenSession / IntentShutdown undefined in server

**Chosen:** Constants exist in `protocol` but `server` handler has no cases.

**Why:** Dead protocol surface or future work — clients must not rely on them until implemented.

## vt10x forked in-tree for scrollback

**Chosen:** Copy `github.com/hinshun/vt10x` into `third_party/vt10x` (MIT, own `go.mod`) and `replace` the import path to it. The fork adds `WithScrollback`, a hook for primary-screen lines lost off the top (scrolls with region top 0, rows dropped by resize). The attach popup keeps them in a bounded ring (`attach_scrollback_lines`, 8 MiB cap) for its scroll mode.

**Rejected:** Reconstructing history in the TUI from the raw byte stream / replay; switching emulators.

**Why:** Upstream is unmaintained and clears scrolled-off lines in place; only the emulator knows when a line leaves the screen. A hook is a small, testable diff (`third_party/vt10x/scrollback_test.go`, run by `make test`); a new emulator would be a much larger change to attach and the daemon's screen mirror.
