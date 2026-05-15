# Plan D — Polish

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. **Commit hygiene: NEVER add `Co-Authored-By: Claude` trailers.**

**Goal:** Close the remaining gaps in the v1 product. Make the modal actually interactive, finish the wizard, ship the settings page + CLI, add the slash palette UI, wire Factorio audio, and turn `:reload` into a real round-trip with daemon SIGHUP handling.

**Out of scope (deferred to Plan E or later):** Lua config layer, theme variants (`noir`, `paper`), row density variants (`compact`, `roomy`), section-slide / modal-scale-in animations, full sound catalog beyond `factorio`, header style "glyphs"/"numbers" rendering.

## Tasks

### D0: Daemon SIGHUP handler reloads tools.yaml
- Modify: `cmd/rex-daemon/main.go` and `internal/server/server.go`
- On SIGHUP, the daemon re-reads `~/.config/rex/tools.yaml` and swaps in a new registry (live sessions keep their existing tool config; only future spawns see the new registry).

### D1: Modal input forwarding
- Modify: `internal/tui/modal.go`, `internal/tui/update.go`
- When focused on the modal, keystrokes are encoded and sent to the session's PTY via `c.SendInput`. `esc` still detaches.

### D2: 5-step wizard (provider → model → effort → name → confirm)
- Modify: `internal/tui/wizard.go`
- Insert effort step (conditional on model having an effort block) and a name step (slug/title/cwd, tab-cycled).

### D3: Settings registry package
- Create: `internal/settings/` (types.go, registry.go, store.go)
- One Setting struct per spec entry; YAML persist at `~/.config/rex/config.yaml`.

### D4: TUI settings page
- Create: `internal/tui/settings.go`
- `S` keybind and `:settings` command open the page. j/k/enter/esc/r.

### D5: `rex config` CLI
- Create: `internal/cli/config.go`
- `rex config list/get/set/reset/edit` against the same registry.

### D6: `/` slash palette UI
- Create: `internal/tui/slash.go`
- Centered fuzzy picker. Built-in entries: `/find`, `/new`, `/help`, `/attach`, `/reply`. Defaults to `/find <typed>` on no match.

### D7: Factorio audio synthesis
- Create: `internal/audio/audio.go`
- Pre-bake PCM for `startup`, `create`, `done`, `delete`, `nav`, `open`, `close` with exponential decay envelopes. `oto/v2` playback.
- Wire from TUI events (modal open/close = `open`/`close`, j/k = `nav`, etc.).

### D8: Smoke test recipe for Plan D
- Create: `docs/superpowers/plans/2026-05-14-rex-polish-smoke.md`

## Acceptance

1. `make test` is green; `make build-all` succeeds.
2. The Plan D smoke recipe passes end-to-end.
3. Modal can be typed into.
4. `:reload` round-trips: edit `~/.config/rex/tools.yaml`, run `:reload`, new tools show up in the wizard.
5. `rex config set spinner braille` writes to config.yaml and is read back by `rex config get spinner`.
6. `/find <query>` filters the board live.
7. Audio fires on startup, session done, and modal open/close (when audio device available).
