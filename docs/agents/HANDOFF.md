# Handoff: start here

For the next coding agent on rex. Read this first, then follow the
onboarding order. Last updated 2026-11-13, after `rex onboard` (ON1),
the shorter agent names and the U7 selector settings; see
[section 7](#7-latest-rex-onboard-2026-11-13) and
[section 8](#8-latest-u7-board-selector-settings-2026-11-13).

## 1. Onboarding order

1. [../../README.md](../../README.md): what rex is, install, first session.
2. [../../CONTRIBUTING.md](../../CONTRIBUTING.md): make targets, test tiers,
   commit and PR checklist.
3. [README.md](README.md): module map and "where to start" per task.
4. [AGENTS.md](AGENTS.md): authority (Go source wins), safe vs careful edits.
5. [../STRUCTURE.md](../STRUCTURE.md): where new code goes.
6. [../../CHANGELOG.md](../../CHANGELOG.md) `[Unreleased]`: what changed
   recently, in user terms.
7. The module doc for the area you touch (`modules/*.md`), then the source.

## 2. The gate

```sh
make check   # vet, race tests, lint (host + windows), coverage floors, govulncheck
```

Run it before every PR. If several worktrees lint at once, golangci-lint can
fail on its lock. Run the linter by hand with `--allow-parallel-runners`:

```sh
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run --allow-parallel-runners ./...
go run -exec 'env GOOS=windows' github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run --allow-parallel-runners ./...
```

Keep the version in step with `GOLANGCI_LINT_PKG` in the `Makefile`.

## 3. Conventions that bit us

- **Conventional Commits**: `type(scope): summary` (see CONTRIBUTING.md).
- **Signed commits**: signing goes through 1Password. If signing fails,
  retry once. Never bypass it (no `--no-gpg-sign`, no config change).
- **Independent review**: every PR gets a review from a separate agent
  before merge. Do not review your own PR and merge it.
- **CHANGELOG**: user-visible changes get an entry under `[Unreleased]`.
- **Coverage floors**: when coverage rises, raise the package's floor in
  `tools/coverage/baseline.txt` to the measured value (`make cover`).
- **Windows CI catches test-only path issues.** macOS/Linux pass, Windows
  fails. Watch for:
  - folder or file names with `"`, `\` or trailing spaces (illegal on
    Windows; skip the case there or pick a legal name);
  - `HOME` vs `USERPROFILE` (set both, or resolve through `internal/paths`);
  - `t.TempDir()` cleanup fails while a child process still runs in it;
    stop the child before the test ends.
- **Shared main checkout.** Several sessions use the main checkout. Work
  in your own worktree (`.claude/worktrees/<name>`) and only fast-forward
  or merge into `integration/todo` there. Before you do: `git status`
  clean, no `.git/MERGE_HEAD`, and send "starting" to any session that
  merges there and wait for its ack. Uncommitted edits in main that no live
  session owns: park them unchanged on their own signed branch, never
  discard them.
- **Known load flakes.** Under a full `make check` on a busy machine (load
  average ~8+), these fail and pass alone: `TestHotRestart*`
  (`cmd/rex-daemon`, 10 s dial), `TestWalk_GitKilledAtBudgetKeepsWhatItListed`
  (`spawnctx`), `TestEvalRequireCycleAndLimits` (`luacfg`, 250 ms timeout).
  The same failures happen on `4d4f6a8`, before ON1. Re-run them with
  `go test -race -run <name> <pkg>` before blaming a change.
- **The repo is public.** Never paste live session output (`rex render`,
  `rex ls`, transcripts, screen dumps) into PRs, issues or commits. Use
  synthetic examples. No real names, emails or personal paths.

## 4. What shipped this round

- #6 vt10x: ignore CSI sequences with `<` `=` `>` markers (attach freeze).
- #7 attach popup render perf; vt10x stops allocating per byte.
- #8 marking a session done plays the done sound.
- #9 `rex prune` and `:prune` clear the Completed column.
- #10 `auto_trust_dirs`: daemon answers Claude Code's folder-trust dialog.
- #11 simpler board: `v` for detail, `tab` next needs-you, Done column folds.
- #12 subagent count column for Claude sessions (`SubagentStart`/`Stop` hooks).
- #13 Windows CI test fixes.
- #14 `rex setup` auto-trust step; refuses `..` paths (security fix).
- #15 Antigravity CLI (`agy`) replaces Gemini; adds `trust_args`, `busy_regex`.
- #16 attach repaint: show a running agent's screen right away (black
  window fix).
- #17 require Go 1.26.11 (GO-2026-6859).
- #19 TUI screensaver: centered `∴ rex` with live counts after idle; keeps
  the macOS display awake (`caffeinate`).

## 5. Open items: where we left off

1. #16 (attach repaint) and #19 (screensaver) are merged. Check
   `gh pr list --state open` for anything newer.
2. Antigravity state: decided, no hooks (never write `~/.gemini`). State
   comes from OSC 7501 (`detect.status: osc7501`, `AGY_CLI_OSC7501=on`),
   with the footer (`busy_regex`) as fallback before the first sequence.
   agy also has a resume id (captured from its exit line) and a
   `trust_prompt` fallback. Not done: subagent count (agy has no source) and
   a preview of the question from the OSC `msg` (kept, not shown).
3. #16 follow-ups:
   - the repaint can snapshot the screen in the middle of an escape sequence;
   - bracketed paste mode (`?2004`) is not tracked by vt10x.
4. #10 (`auto_trust_dirs`) and #12 (subagent count) were not tried against
   a live Claude run with background subagents.
5. ~~`rex config set auto_trust_dirs` does not validate its value.~~ Done:
   `Setting.Normalize` runs in `Store.Set`, with the same checks as
   `rex setup`.
   The rest of the CLI hardening pass (P3) is done too: flags anywhere,
   `-h` exits 0, `setup` writes spawn defaults, absolute `-cwd`, safe
   terminal detection with `rm` needing `-force`, `wait` validation, clear
   daemon-down errors (exit 4), slug/short-id collisions (exit 3), unknown
   command (exit 5). Breaking changes are listed under **Changed** in the
   CHANGELOG. New commands must parse with `core.ParseArgs` and build errors
   with `core.Errorf(code, hint, ...)`.
6. More unchecked TODO items live in the user's local, gitignored `TODO.md`
   (not in the repo). Ask the user for it.

## 6. Doc debt noticed

- `docs/agents/` is a map and can lag the code; Go source wins.
- `IntentOpenSession` is still a protocol constant with no server handler
  (`bad_intent`); documented as such in `docs/protocol.md`.
- `docs/agents/modules/*.md` were not re-checked line by line for
  PRs #6–#17. Check the module doc against source before relying on it.

## 7. Latest: `rex onboard` (2026-11-13)

On `integration/todo` (not pushed; ahead of `origin`):

- `522ed38`, `678e55a`: **`rex onboard`**. Spec and decisions:
  [../plans/ON1-onboard.md](../plans/ON1-onboard.md). User docs:
  [../cli.md](../cli.md#rex-onboard).
  - Five-step tour: install checks (starts the daemon), coding agents
    (installed with path, missing with `install_hint`), `rex setup` on `s`,
    board keys (from `tui.BoardKeyDesc`, so the tour follows `?`), next
    steps. A failed check blocks the tour; `r` rechecks. Finishing writes
    `~/.local/share/rex/onboarded` (`paths.OnboardedFile`).
  - `rex onboard -check`: the checks without prompts, all formats, exit 1
    with no usable agent. Outside a terminal the tour exits 5.
  - Stale daemon `PATH` (CLI finds an agent, daemon does not): warning
    pointing at `rex daemon restart -cold` (hot restart keeps the old env).
  - Code: `internal/surface/cli/setup/onboard.go`, `agents.go`; test seams
    `onboardDaemonTools`, `onboardStartDaemon`, `onboardRunSetup`,
    `onboardInstallChecks`.
- **Live agent detection.** `Server.RescanInstalled` re-probes `PATH` (one
  `LookPath` per tool) and sends `ToolsChanged` only when an installed bit
  changes. Runs every 30 s (`Catalog.WatchInstalled`) and on the new
  `RescanTools` intent, which the board sends via `Do` when the new-agent
  page opens (an older daemon's `bad_intent` reply is dropped, never shown).
- `rex doctor` builds its `tool:` rows from the registry and **fails** with
  no usable agent (`agents installed`). Intended (spec); CI or containers
  without an agent will see exit 1.
- The board opened with no config and no marker shows "new to rex? quit (q)
  and run rex onboard" (`meta.firstRunNotice`).
- `8fe3edd`: shorter agent display names (Codex, Antigravity, Ollama, Kimi).
  Found uncommitted in main with no owner, parked, reviewed, merged.

Open:

1. Push `integration/todo` when the user says so.
2. The interactive tour was tested with scripted stdin only, never driven
   in a real terminal. Try `rex onboard` by hand once.
3. Settings → Onboarding and the new-agent page "N more" row predate ON1;
   unchanged by decision. Revisit if users want missing agents hidden.
4. `RescanTools` is not exposed as a CLI command; `rex reload` still works.

## 8. Latest: U7 board selector settings (2026-11-13)

On `integration/todo` (not pushed):

Plan and decisions: [../plans/U7-selector-glyph-gutter-blink-settings.md](../plans/U7-selector-glyph-gutter-blink-settings.md).
Commits `5dbf94e`, `d15d7a1`, `46f6c4b` (+ TODO line `e73021e`).

- Five Appearance settings: `selector_glyph` (`theme` default, presets
  `▸ ❯ ➜ ➤ ☞`, free entry 1–2 cells), `selector_gap` (1–3, default 2),
  `selector_bold` (on), `selector_blink` (`off`/`blink`/`pulse`),
  `selector_blink_ms` (200–2000, default 600). All five are in
  `WorkspaceAllowIDs`.
- `internal/surface/tui/glyphs.go`: `curSelector` is loaded from the Store
  by `syncSelector(m.Store)` at the top of `View`. `selectGlyph()` →
  setting, else theme `glyphs.select`, else `▸`; `ascii_glyphs` turns a
  non-ASCII glyph into `>`. `selectCursor()` is the 2-column picker cell
  every picker uses (no hardcoded `▸ ` left in the TUI).
- Board: `rowIndent()` is a func now (was a const 3); the gutter is
  `1 + glyph width + selector_gap`. Fleet bars and blanks pad to the slot.
- Blink lives in `selector.go`: phase counts from `Model.lastInput`, so
  any key leaves it solid; it rests after `selectorIdle` (1 min), off the
  board, under the screensaver and with `reduce_motion`.
  `wantsSpinnerTick` includes `selectorBlinking`, so an idle board with
  blink off never ticks.

Open:

1. Follow-ups:
   - `feat/new-agent-steps` (another agent, unmerged on 2026-11-13)
     rewrites `newagent_view.go`, `newagent_agent.go`, `keys.go` and cuts
     `wizard.go` down. It must keep calling `selectCursor()` in the new step
     views; check for a reintroduced hardcoded `▸ ` after it lands.
   - `☞` is East Asian *ambiguous* width (user decision D1 in the plan). A
     terminal set to draw ambiguous characters wide shifts the selected row
     one column. Not fixable without knowing the terminal; `👉` is the safe
     2-cell finger.
   - A theme file's `glyphs.select` must be 1 cell, but `selector_glyph`
     takes 2. Harmless (the board slot follows the width), just
     inconsistent.
   - Tests that render a picker without going through `View` see whatever
     `curSelector` the last test left. Reset it with
     `t.Cleanup(func() { curSelector = selectorDefaults })` (see
     `selector_test.go`).
2. Known `make check` flakes under full-suite load, also on older bases:
   `cmd/rex-daemon` `TestHotRestart*` and `spawnctx`
   `TestWalk_GitKilledAtBudgetKeepsWhatItListed`. Rerun the package alone
   (`go test -race -count=1 ./cmd/rex-daemon/`) before treating them as
   real. `cover-check` also prints "2 package(s) have no floor" (`e2e`,
   `pty/ptytest`); that is a warning, exit 0.
3. Manual TUI check without touching the real config or daemon: build
   `rex` and `rex-daemon` into a temp `bin`, then run with `HOME=<tmp>`,
   `XDG_RUNTIME_DIR=<short dir>` and that `bin` first on `PATH`.
   - Keep the socket path under ~100 bytes: a long `XDG_RUNTIME_DIR`
     fails with `connect: invalid argument`.
   - Use `rex daemon start`, then `rex new -no-attach "<prompt>"` (the
     built-in `echo` tool, no real agent), and `rex config set ...`.
   - Drive the TUI in a private tmux server: `tmux -L <name> new-session
     -d -x 110 -y 22 rex`, then `send-keys` and `capture-pane -e -p`. The
     `-e` keeps the colors, so you can see the blink and pulse.
4. The user has not yet run `make install` + `rex daemon restart` to try
   U7 in their own rex; the installed binary predates it.
