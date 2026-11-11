# Changelog

All notable changes to rex are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- `rex setup` asks whether agents may skip the folder trust prompt in a
  folder (default no; it suggests the folder that holds the current
  project), and `rex setup -auto-trust folders` sets it without the
  question. Both write `auto_trust_dirs` and refuse the values the daemon
  ignores (a filesystem root, the home folder, relative paths, `..`), so
  setup and the daemon share one set of checks.
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
