# Changelog

All notable changes to rex are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- The daemon tracks a resume id per session (`resume_id` in snapshots,
  state patches and `meta.json`). A Claude Code session is started with
  `--session-id <uuid>`, and its SessionStart hook keeps the id current
  after `/clear` or an in-app `/resume`. Tools declare how their id is
  found and used in a new `resume` registry block (`args`, `new_args`,
  `capture_regex`, with `{id}` as the placeholder); see `docs/registry.md`.
  Only canonical UUIDs are accepted, since the id ends up on a command line.
  Nothing uses it yet: resuming an exited session comes next.
- TUI: the detailed view (`v`) has a start→end span column (`14:02→15:30`,
  `14:02→` while live). The end is when the row entered Done: the earlier of
  marking it done and the session ending. It is the first column to go on a
  narrow terminal.
- Setting `board_hidden_sections` (Behavior): section ids hidden at startup,
  for example `done`. `:bg` now also saves the hidden sections, and the
  next start restores them.
- TUI: the filter bar names the hidden sections (`done hidden (zh)`) and
  stays up while any is hidden. `:prune` says "filters ignored" in its
  confirmation while a filter is active, since it clears every Done
  session.
- TUI: `:filter hide <section…>`, `:filter show <section…|all>` and
  `:filter only <section…>` hide and show board sections by id, prefix,
  title or number. A bare `:filter` clears the tool filter, every hidden
  section and the search.
- TUI: board search. `/` opens an editor on the bottom line and narrows the
  board as you type: a row must contain every word, case-insensitive, in
  its name, task, AI summary, tool, model, slug, short id, fleet, folder
  name or resume id. `enter` keeps the query, `esc` restores the previous
  one, and `esc` on the board clears it. The filter bar shows
  `/auth · 3 matches · esc clear` and counts matches inside hidden
  sections. A search unfolds a long Done column. `:search [text]` does the
  same from the command line (`::search` from attach), and `:bg` saves the
  search for the next start.
- TUI: `uc` and `:uc [sel]` undo done: they unmark a running session that
  was marked done (`cc`, `:done`). A session that is not in Done gets a
  notice, and one that has already exited is explained rather than touched.
  `u` then `c` never arms `cc`.
- TUI: section titles read `n/total` (`WORKING  1/4`) while a tool filter or
  search narrows the rows.
- TUI: hide board sections. `zh` hides or shows the completed (Done)
  section, `z1`-`z9` toggle section N, `z0` shows them all. While `z` is
  pending the help line lists the second keys. `1`, `3` and `tab` aimed at
  a hidden section say how to show it, and rex refuses to hide every
  section.
- `-o/--output text|txt|md|json|agent`, `--agent` and `--no-color` for
  `rex ls` and `rex status` (more commands follow). `json` is a versioned
  `{"v":1,...}` document, `md` a GitHub table, `agent` one `key=value`
  line per record plus an `ok` line, and `txt` the plain columns rex has
  always printed in a pipe. Errors follow the format on stderr. Colors
  follow the `color_scheme` setting, `NO_COLOR`, `CLICOLOR_FORCE` and
  `REX_ASCII` are honored, and stderr is never colored by stdout's
  terminal. New environment: `REX_OUTPUT`, `REX_AGENT`, `REX_ASCII`. The
  existing `-json` stays as a legacy alias with its current shapes.
  Nothing in the UI uses it yet.
- New `Resume` intent (`client.Resume`). On a running session it removes the
  done mark. On a done, failed or crashed session, including ones stranded by
  a daemon restart, it starts the agent again in the same row with the tool's
  resume arguments, keeping the id, slug, start time and transcript; folder
  trust is recomputed from the current config. It fails with `bad_state` when
  the tool has no resume support or no resume id is known, and with
  `too_many_sessions` at the session cap. A `Delete` racing a resume leaves no
  process and no directory behind. See `docs/protocol.md`.
- The daemon records `ended_at` for every session: the first time it reached
  done, failed or crashed. It is in `meta.json`, in snapshots and in state
  patches, and older sessions get it from their last event.
- `rex setup` asks whether agents may skip the folder trust prompt in a
  folder (default no; it suggests the folder that holds the current
  project), and `rex setup -auto-trust folders` sets it without the
  question. Both write `auto_trust_dirs` and refuse the values the daemon
  ignores (a filesystem root, the home folder, relative paths, `..`), so
  setup and the daemon share one set of checks.
- TUI: screensaver. After 5 minutes with no key or click on the bare
  board, the board gives way to a small centered `∴ rex` with the live
  counts under it (`1 need you · 3 working · 7 done`). Any key or click
  brings the board back and does nothing else. Overlays, prompts and the
  attach popup never time out. On macOS it keeps the display awake while
  it shows (`caffeinate -d -w <rex pid>`, stopped on wake and on exit).
  It moves a little once a minute, and stays put with `reduce_motion`. New
  settings: `screensaver_enabled` (default on),
  `screensaver_after_minutes` (default 5, 1–120) and
  `screensaver_keep_awake` (default on, macOS only).
- Antigravity CLI (`agy`) replaces Gemini CLI, which Google retired on
  2026-06-18, as a built-in tool (`antigravity`). It ships the account's
  current models (Gemini 3.8 Flash with low/medium/high effort, Claude
  Sonnet/Opus 4.6, GPT-OSS 120B) and takes the first prompt with
  `--prompt-interactive`. Under `auto_trust_dirs` it starts with the folder
  trusted for that session only (`--settings`, never saved), so no trust
  question shows. Tools can declare this with the new `trust_args`. Its
  state comes from the screen's footer, using the new optional
  `busy_regex` so a quiet stretch mid-turn stays working. `gemini` stays
  built in but hidden, so existing setups keep working.
- TUI: a Claude Code session running subagents shows how many in its row
  (`⋔3`). The daemon tracks them with Claude's `SubagentStart` and
  `SubagentStop` hooks; the wire gains an optional `subagents` field.
- TUI: marking a session done (`cc`, `:done`, `rex done`) plays the done
  sound. The session keeps running, so nothing announced the mark before.
  `cc` no longer also plays the close sound for a mark; undoing still does.
- `rex prune` and the TUI's `:prune` delete every Completed session (done,
  failed, crashed, marked done) after one confirmation, keeping sessions
  marked done whose agent is still working. Each session is checked again
  just before its delete, so one typed into after the list was shown is
  kept. `rex prune -dry-run` lists them; `-yes` skips the question and is
  required without a terminal.
- `auto_trust_dirs` setting: for sessions started in these folders, the
  daemon answers Claude Code's "trust this folder" dialog (it moves to
  "Yes, I trust this folder", checks it is selected, then confirms).
  Empty by default; set it with `rex config set auto_trust_dirs ~/dev`.
  Tools declare their dialog with `trust_prompt` in `tools.yaml`. Only
  list folders whose code you trust; see `SECURITY.md`.
- Protocol: the daemon answers every intent that carries an envelope `id`
  with exactly one reply: its specific event (`Hello` gets `Snapshot`),
  an `Error`, or the new `Ack` event. Intents without an `id` behave as
  before. `Snapshot` carries the daemon's version (`daemon_version`) and
  `Hello` the client's. See `docs/protocol.md`.
- Hardening effort under way: a staged pass over crash, hang, data-loss and
  security findings from a full code audit. Entries for each fix land here.
- `make lint-windows`, `make cover`, `make cover-check`, `make fuzz` and
  `make test-e2e`. `make check` now also runs the Windows lint and the
  per-package coverage floors in `tools/coverage/baseline.txt`.
- CI: nightly race stress run of the daemon and wire tests, and a weekly
  fuzz run.
- `SECURITY.md` (private reporting and threat model), `CONTRIBUTING.md`,
  CODEOWNERS and a pull request template.

### Changed

- TUI: an unknown `:` command now says `(try :help)`. Commands and key
  help come from one table, so `?` can no longer drift from what `:` runs.
- `rex ls` and `rex status` on a terminal use the new human layout (state
  markers, color, width-aware columns). Piped output is unchanged.
- The `skip_permissions` setting now applies only in folders under
  `auto_trust_dirs` (also when a session is resumed). In any other folder
  agents start without `--dangerously-skip-permissions` and the daemon log
  says so.
- `NewSession` for a tool or model that is not in the registry now fails
  with `bad_intent` (it was `spawn_failed`), the same error `Resume` gives.
- Skipping agent permission prompts is now opt-in. Claude Code and
  Antigravity no longer start with `--dangerously-skip-permissions` unless
  you set `skip_permissions: true` (default off; read at each spawn). The
  flag moved from the built-in `command` to a per-tool
  `skip_permissions_args` registry field.
- CLI: flags are now accepted anywhere among the arguments, so
  `rex new task -tool claude`, `rex wait p1 -until done`,
  `rex done p1 -after 72h` and `rex attach p1 -read-only` work as
  documented. `--` ends flag parsing. **Breaking:** free text that starts
  with `-` (in `rex new`, `rex reply`, `rex name`) now needs `--` before
  it, for example `rex name p1 -- -clear`; an unknown `-x` after text exits 5
  with a hint. `-h` on any command prints its usage and exits 0 instead of
  5.
- TUI: a simpler board. Rows show state, name, the task (or the agent's
  question when it is waiting on you) and age; `v` brings back the id,
  model, tokens and AI summary. Columns are NEEDS YOU / WORKING / DONE
  with counts, the header reads `N need you · N working · N done`, and
  the help bar lists five keys (`enter open · i new · cc done · tab next
  · ? more`). `tab` jumps to the next session that needs you. The tool
  filter row only shows when sessions use more than one tool. A Done
  column longer than 5 rows folds into its title (`3` shows it). No
  existing key changed.
- Lint: gosec, errorlint, exhaustive, gocritic, noctx, contextcheck,
  nilerr, bodyclose, copyloopvar and durationcheck are enabled. gosec's
  directory and file permission checks (G301, G302, G306) are enforced:
  golangci's `legacy` exclusion preset, which hid G301 and G302, is no
  longer used. Lint exclusions that matched nothing are removed.
- CI: superseded PR runs are cancelled, every job has a timeout, and the
  Linux job runs the full `make check` gate.
- Coverage floors in `tools/coverage/baseline.txt` are raised to the
  measured coverage. `cmd/rex-daemon`, `internal/daemon/boot` and the test
  helpers now have floors.
- `rex daemon stop`, `status` and `restart` act on whatever daemon answers
  on the socket. A PID is shown for information only, as `pid unknown`
  when it cannot be determined. `restart` no longer sleeps between stop and
  start, and it does not start a second daemon if the first will not stop.

### Fixed

- TUI: `::undone` in the attach popup only unmarks; on a session that is not
  marked done it shows a notice instead of marking it done.
- The settings page showed a second "Spawn" section for
  `auto_trust_dirs`; it now sits with the other Spawn settings.
- TUI: a reconnect to the daemon no longer resets the tool filter to `all`.
- TUI: `:filter <tool>` now moves the selection off a row the filter hides
  (before, `enter` did nothing) and rejects a tool that is neither a filter
  chip nor on the board, with `filter: unknown tool or subcommand x`.
- `rex ls` no longer prints raw slugs and names: control and bidi
  characters are flattened, like in the TUI.
- The attach popup draws Antigravity (and other ultraviolet-based agents
  under a tmux, ghostty or kitty `TERM`) correctly: the logo is centered and
  no row is cut off on the right. rex's terminal emulator ignored the "repeat
  last character" sequence (`CSI n b`) and lost its tab stops beyond the
  width a screen was first created with, so every later cell on a row was
  drawn too far left or jammed at the right edge. It now handles both, and
  `CSI ? 5 W`.
- A session that ends now always sends `working_since: null` in its final
  state patch, as the protocol says. It was lost on the path where the child
  exits or is completed, so a client could keep a stale working timer.
- A partial `tools.yaml` entry for a retired built-in tool (codex, gemini,
  ollama, grok, deepseek, kimi) no longer stops the daemon from starting.
  It is dropped with a warning in the daemon log and on the TUI splash.
- Saving settings no longer overwrites changes made elsewhere. `Store.Save`
  re-reads `config.yaml` and writes only the keys that store changed (a
  reset deletes its key), so a TUI save can no longer undo a concurrent
  `rex config set`. New config files hold only the keys that were set; keys
  this build does not know are kept. Existing files are not trimmed.
- `rex config set auto_trust_dirs` (and the settings page) now refuse the
  values `rex setup` refuses (a filesystem root, the home folder, relative
  paths, `..`), exit 5 and leave `config.yaml` unchanged, instead of saving
  a value the daemon ignores.
- `rex ls` and `rex digest` clip long model, slug and name columns by
  display width instead of bytes, so multi-byte text such as `·` or CJK
  is no longer cut mid-character and the columns stay aligned.
- TUI: the `λ` prompt glyph, the `▸` selection marker and the `:` prompt
  are now colored and follow `color_scheme` (they were never colored).
  The text cursor is drawn in reverse video, or as a solid `█` when there
  is no color (`NO_COLOR`), so it is always visible. The color scheme and
  the style cache are no longer written from background commands at
  startup, which removes a data race with the splash screen.
- TUI: `:undone` on a session that is not marked done no longer marks it
  done. It now only unmarks, and says "<name> is not marked done"
  otherwise. `:done` still toggles.
- TUI: opening a running full-screen agent (Claude Code, codex, gemini, …)
  no longer shows a black popup until the agent next draws. The popup
  attaches without the transcript replay and relied on the agent redrawing
  on `^L`, which a busy agent, or one that ignores `^L`, did not do. The
  daemon now repaints its own copy of the agent's screen into the popup
  (colors, cursor and terminal modes included) when it opens. `rex attach`
  gets the same repaint after the transcript tail, which could start
  mid-frame (or was empty with `-no-replay`). The `^L` nudge is now sent
  only to a daemon too old to repaint. Protocol: `Subscribe` gains an
  optional `repaint` field and `SessionOutput` an optional `repaint` flag;
  older clients and daemons are unaffected. Subscribing no longer loses or
  repeats the output that arrives while the daemon reads the replay.
  `rex attach` also resets the scroll region, charset and cursor-key and
  keypad modes on detach.
- A character that arrives split across two reads of the agent's output
  (box drawing, emoji, accented letters) is no longer lost, along with the
  rest of its row shifting left, in the attach popup and in the daemon's
  copy of the screen (board last line, AI summaries, the repaint above).
- TUI: the attach popup no longer drifts out of sync with a long-running
  Claude Code session until you detach and reattach. The terminal emulator
  read Claude's kitty keyboard sequences (`CSI < u`, `CSI > 5 u`) as
  "restore cursor", so each redraw landed on the wrong rows and the popup
  kept showing an old frame. Sequences with a `<`, `=` or `>` marker are now
  ignored. The daemon's per-session screen, which feeds the board's last
  line and the AI summaries, had the same drift and is fixed too.
- TUI: the attach popup renders about 5× faster with 5–10× fewer
  allocations per frame (211×57: 3.2 ms to 0.6 ms on an unchanged screen,
  3.6 ms to 0.8 ms with new output), so busy agents no longer make it choppy. It
  paints the emulator grid directly instead of through a lipgloss style
  per cell, reuses unchanged rows, and draws the popup border in one pass.
- TUI: the attach popup shows bold, italic, underline and reverse text,
  including an agent's own reverse-video cursor, and 24-bit colors. It
  used to drop the attributes and send 24-bit colors as invalid 256-color
  codes.
- Daemon and TUI: the terminal emulator no longer allocates on almost
  every byte of agent output (14× fewer allocations on escape-heavy
  output). The daemon runs one per session.
- CLI: when the daemon is unreachable, the error names the socket once
  (`dial unix <path>: connect: …`) instead of wrapping it twice.
- `rex reload` and the TUI's `:reload` report a `tools.yaml` the daemon
  could not load and exit 1. They used to exit 0 with the error only in
  the daemon's log. A daemon older than this release still gets the
  reload, unacknowledged.
- Paths: every rex path now comes from one package
  (`internal/runtime/paths`, documented in `docs/paths.md`). File
  locations are unchanged. An unset or relative `HOME` is now an error
  that names the flag to pass (`-socket`, `-config`, `-state-dir`),
  instead of reading and writing paths relative to the working directory
  (or, for `rex digest`, a shared `/tmp/rex`). A relative
  `XDG_RUNTIME_DIR` is ignored, as the XDG spec requires.
- `.gitignore` no longer hides source files: bare `store_test.go`,
  `persist_test.go` and `test/` patterns silently ignored matching files
  anywhere in the tree.
- The CLI exit code is now found through wrapped errors.
- Daemon: a client that stops reading no longer freezes the daemon. Each
  connection has its own bounded outbound queue.
- Daemon: shutdown completes while clients are connected, and a second
  SIGINT or SIGTERM forces the exit.
- Daemon: a second `rex-daemon` on the same state directory exits and
  names the running daemon's PID instead of taking over its socket.
- Daemon: a panicking intent handler closes only its own connection.
- Daemon: a full disk while writing a transcript no longer orphans the
  agent process or hangs Delete. The agent is killed and reaped.
- Daemon: a daemon started by `rex` or the TUI survives Ctrl-C in that
  terminal, leaves no zombie process, and an early crash is reported with
  the end of its log.
- TUI: when the daemon goes away, the TUI reconnects with backoff instead
  of spinning at 100% CPU.
- TUI: `:new` no longer locks up the TUI.
- TUI: `rex setup` no longer crashes on a broken `tools.yaml`; it falls
  back to the built-in tools.
- TUI: queued sessions show in the Working column and in the header
  counts.
- TUI: a data race when the summarizer model is swapped is fixed.
- TUI: `:reload` reloads the daemon on the TUI's own socket instead of
  the default one, and a failed reload shows in the status line.
- CLI: `rex uninstall -strip-profile` no longer truncates a profile with
  an unterminated `# BEGIN REX`. It writes a `.rex-backup-<unix>` copy,
  keeps symlinks, CRLF line endings and long lines, and also scans
  `~/.profile`.
- CLI: `rex uninstall` stops the daemon and confirms it is gone before it
  removes anything. The new `-force` flag continues anyway.
- CLI: `rex update` refuses a binary-swap install before it stops the
  daemon.
- CLI: `rex attach -read-only` can detach with Ctrl+].
- CLI: `rex log` strips every escape sequence, including in `-f` mode.
- CLI: commands no longer print log lines into the terminal or the attach
  screen.
- CLI: `rex daemon stop` no longer refuses when the user runs more than one
  daemon (for example on different sockets).
- Tests: the e2e suite no longer fails at random under `go test -race
  ./...` on macOS. Every script hard-linked the same `rex-daemon`, and
  Gatekeeper killed the daemons when it looked the shared file up by the
  path of a script that had already finished. Each script now runs its
  own copy.
- Tests: the PTY orphan tests no longer send SIGKILL to a PID read back
  from a file unless that process is still the test's own child.

### Security

- rex now requires Go 1.26.11: it fixes GO-2026-6859 in `net/http`,
  which govulncheck reports as reachable from rex.
- The daemon's state directory, socket and logs are owner-only (0700
  directories, 0600 files), and existing ones are tightened. Connections
  from other users are rejected. The TUI state file, the daemon log the
  TUI and setup open, and the directories `rex doctor`, `rex config edit`
  and the boot splash create are owner-only too.
- Saving settings or installing the shell block no longer makes
  `config.yaml` or a shell profile world-readable: a rewrite keeps the
  file's existing mode, and a new file is 0600.
- Session IDs are validated before any path is built from them, which
  blocks path traversal.
- Protocol lines are capped at 16 MiB. Protocol errors are reported to the
  client before the connection is closed, with the new codes
  `line_too_long`, `unsupported_version` and `internal_error`.
- Session titles, fleets, summaries and session text are stripped of
  terminal escape sequences and control characters, both when the daemon
  receives them and when the TUI renders them. The daemon, the TUI and the
  AI summarizer share one sanitizer, so the TUI now also removes bidi
  overrides and drops C1 escape sequences (such as U+009B) whole.
- Supervisor logs redact agent command-line arguments.
- A runaway `init.lua` can no longer hang the daemon: Lua handlers run off
  the hot path with timeouts. `init.lua` is documented as trusted code.
- `rex daemon stop`, `rex reload` and the TUI's `:reload` act only through
  the daemon's socket. They never signal a process found by name.
- `auto_trust_dirs` entries that start with `~/` and contain `..` are now
  ignored like any other path with `..`. Expanding `~/` cleaned the path
  before the check, so `~/..` trusted the folder above home (such as
  `/Users`) and `~/a/../b` trusted `~/b`. A `HOME` that contains `..` no
  longer lets `~/` trust the home folder, and while `HOME` is set but
  cannot be resolved, no folder is trusted.

## [v1]

Baseline release: `rex` CLI and TUI, `rex-daemon` PTY supervisor, JSONL
protocol over a Unix socket, tool registry, Lua hooks, and native Windows
support (ConPTY).

[Unreleased]: https://github.com/tristanbietsch/rex/commits/master
[v1]: https://github.com/tristanbietsch/rex/commits/6a6e0a1
