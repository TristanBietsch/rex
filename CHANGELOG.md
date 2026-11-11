# Changelog

All notable changes to rex are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- `rex agent-help` (also `rex help agent`, and `rex --help` in agent mode)
  prints a compact usage card for LLM agents: line grammar, exit codes, the
  common commands and the interactive ones to avoid. `-o json` gives it as
  data.
- `rex help <command>` is the same as `rex <command> -h`.
- `rex resume <sel>` respawns an exited session in place (same row, id, slug
  and transcript) with the agent's resume arguments, or unmarks a running
  session marked done. `rex resume -print <sel>` prints only the full resume
  id. It exits 5 with `off: enable in Settings → Features (rex config set
  resume true)` while the `resume` feature is off.
- `rex ls -wide` adds a `RESUME ID` column (full id) while the `resume`
  feature is on.
- `rex ls -wide` adds `STARTED` and `ENDED` columns before `NAME` (agent
  lines get `started=` and `ended=`).
- Setting `timestamps` (Features, on by default): switches off the TUI's
  start→end span column and the `rex ls -wide` time columns. The end time
  is still recorded.
- TUI: a dim preview line under the board shows the selected session's full
  AI summary and how old it is (`↳ Editing api/webhooks.go … · 12s`), in
  both views, while any session has a summary. In the detailed view it only
  appears when the row cuts the text. Two lines on a tall terminal at
  `summary_detail: long`. Hidden below 20 rows, and switchable in Settings →
  Features (`summary_preview`).
- TUI: descriptions are cut on a word boundary with an ellipsis, and the
  description animation ends on exactly the settled text.
- Setting `summary_detail` (AI summary): `short`, `medium` (default) or
  `long` sets how much an AI description says (about 8, 15 or 28 words; 60,
  110 or 180 characters). The daemon applies a change within about 5
  seconds, with no restart, by polling `config.yaml`.
- Sessions carry `description_at`, when the AI description was last written
  (snapshots, `description` patches, `meta.json`). Older clients ignore it.
- Settings: a new Features section (after Behavior) holds one on/off switch
  per optional feature. A feature that is off is inert: it starts nothing,
  writes nothing, its keys do nothing and its commands refuse. No switches
  yet; each feature adds its own. `settings.FeatureOn(store, id)` reads one.
- `NewSession.trust_folder` (daemon protocol): trusts the session's folder
  for that session only, as if it were in `auto_trust_dirs`. The daemon
  answers the agent's trust dialog (or passes `trust_args`), and
  `skip_permissions` applies there. Refused (`bad_intent`) for relative
  paths, `..`, a filesystem root and the home folder. The choice is saved in
  `meta.json` (`trust_folder` in summaries) and kept when the session is
  resumed. The CLI and TUI controls follow.
- Setting `board_filter` (Features, on by default): switches off the tool
  filter chips, `:filter`, `t`, the `z` chords and `board_hidden_sections`.
  Off, there is no chip row and `:filter` answers `off: enable in Settings
  → Features (rex config set board_filter true)`.
- Setting `search` (Features, on by default): switches off board search.
  Off, `/` does nothing, `:search` answers the off message, and a saved
  query is ignored.
- Setting `undo_complete` (Features, on by default): switches off `uc` and
  `:uc`. Off, the chord is unbound and the command answers the off message;
  `:undone` still unmarks.
- Setting `resume` (Features, on by default): switches off resume ids. Off,
  the daemon assigns and captures no id, the column and `:rid` go away, and
  `Resume` is refused with `resume is off`.
- TUI: the detailed view shows each session's resume id (first 8 characters)
  in a column that is the first to go on a narrow terminal, and `:rid [sel]`
  shows the full id on the status line.
- The daemon tracks a resume id per session (`resume_id` in snapshots,
  state patches and `meta.json`). A Claude Code session is started with
  `--session-id <uuid>`, and its SessionStart hook keeps the id current
  after `/clear` or an in-app `/resume`. Tools declare how their id is
  found and used in a new `resume` registry block (`args`, `new_args`,
  `capture_regex`, with `{id}` as the placeholder); see `docs/registry.md`.
  Only canonical UUIDs are accepted, since the id ends up on a command line.
 
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
- TUI: `uc` and `:uc [sel]` (also `:resume [sel]`) undo done: they unmark a
  running session that was marked done (`cc`, `:done`), and resume an exited
  one (done, failed, crashed, or completed with `C`) in place with its
  resume id: same row, slug, start time and transcript, back in Working. An
  exited session whose tool can't be resumed is explained rather than
  touched. A session that is not in Done gets a notice. `u` then `c` never
  arms `cc`.
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
- `rex doctor` shows the installed agy version (`agy 1.5.5`, a `-verbose`
  row) and warns below 1.5.5 (untested). It shows nothing when agy is not
  installed, and gives up after 2 s.

### Changed

- `rex digest`, `rex stats` and `rex fleet` take `-o`/`--agent`; `rex log`
  and `rex render` take `-o` for text and txt (md and json exit 5). Piped
  `digest` and `stats` output (`txt`) no longer has the banner and rule:
  label and value lines under `# Section` headings. `-json` keeps its
  shape; `-o json` is the same data in the versioned envelope with integer
  millisecond durations. Digest errors name `-date`/`-since` (single dash).
- The session commands (`new`, `reply`, `send`, `wait`, `rename`, `name`,
  `done`, `undone`, `archive`, `complete`, `rm`, `prune`) take `-o`,
  `--agent` and `--no-color`: `text` prints a `✓` line, `json` the result as
  data, `agent` one `ok cmd=… id=… slug=…` line. `txt` (a pipe) prints what
  each command always printed. Confirmation questions and warnings go to
  stderr.
- A declined `rex prune` question now exits 6 (it was 1), like `rex rm`.
  `prune` and `rm` need `-yes`/`-force` in every format but `text`, and
  whenever stdin or stderr is not a terminal.
- Help is rebuilt in the style of `agy`: `rex --help` and `rex <command> -h`
  start with `Usage:`, use Title-case sections with a colon, align their
  columns and end with one hint, with no banner or rule. They are generated
  from one command table, print to stdout (`-h` used to print on stderr) and
  follow `-o` (`txt` in a pipe, `md`, `json`, `agent`). A flag error prints
  the error and a one-line `Usage:` hint instead of the whole flag list. The
  completion scripts now list the same commands as `--help`.
- `rex setup` now suggests trusting the project's own folder (the git root
  of where you ran it, or the folder itself when it is not a checkout)
  instead of the folder above it. Still never your home folder or anything
  outside it. The logic moved to `settings.SuggestTrustDir`, with
  `settings.AppendTrustDir` and `settings.RemoveTrustDir` beside it.
- **Breaking:** an unknown command now exits 5 (was 1) and, when a real
  command is within two edits, says `did you mean "ls"?`.
- **Breaking:** `rex reply <sel>` with no text on a terminal exits 5 instead
  of waiting for end-of-file; `rex log -n` with a negative number exits 5
  instead of being accepted.
- `rex new` waits for the daemon with `-wait` (default 5s). On timeout it
  says `daemon did not confirm the spawn within 5s; check \`rex ls\`` and
  exits 1, instead of a raw deadline error.
- `rex setup -y` is a real alias of `-yes` (so `-y=false` works). The no-op
  flags `rex ls -short`, `rex ls -show-archived` and `rex new -no-attach`
  are marked "(deprecated, no effect)" in their usage; they are still
  accepted.

- **Breaking:** a selector that is a session's exact slug and also the short
  id (or id prefix) of a different session, such as a slug `feed` and
  another session whose id starts with `feed`, now exits 3 and lists both.
  It used to pick the slug silently, so `rex rm feed` could delete the wrong
  session. Use the full id.

- **Breaking:** `rex rm` without a terminal on stdin now needs `-force`
  (aliases `-yes`, `-y`) and otherwise exits 6, like `rex prune`. It used
  to delete without asking. Declining the prompt on a terminal also exits 6
  (was 1).

- AI descriptions are more specific and use the session state: working,
  waiting for you (they say what is needed), finished or failed. They never
  start with the agent's name ("Claude is…"), are sentence case, and the
  default length is longer (`summary_detail: medium`, 110 characters).
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

- `auto_trust_dirs` and the per-session trust request now also refuse a case
  variant of the home folder on a case-insensitive filesystem (macOS,
  Windows), and any folder that holds the home folder (`/Users`, `/home`).
  Folders are compared by identity on disk, and ancestors by whole path
  elements, so `/Users2` is still fine.
- TUI: `::undone` in the attach popup only unmarks; on a session that is not
  marked done it shows a notice instead of marking it done.
- Short ids match in any case (`rex attach A91C`). `rex --help` no longer
  lists the `@needs`, `@working` and `@done` selectors, which never existed.

- With no daemon running, commands now say so: `rex-daemon is not running
  (no socket at <path>); start it with \`rex daemon start\``, instead of the
  raw `dial unix …: no such file or directory`. The same goes for a socket
  file a dead daemon left behind. A daemon that accepts the connection but
  fails the handshake exits 4 (it was 1) in selector lookup, `rex prune` and
  `rex render`, which also returned raw errors.

- `rex wait` refuses an unknown `-until` and a negative `-timeout` (exit 5)
  instead of waiting forever or without limit. A timeout is recognized by
  its error type, not its text. `docs/cli.md` said a session removed during
  `wait` exits 3; it exits 2.

- `rex new`, `rm` and `prune` no longer crash when stdin is closed, and a
  character device that is not a terminal (`< /dev/null`, `< NUL` on
  Windows) is no longer mistaken for one.

- `rex new -cwd` makes a relative folder absolute and refuses one that
  does not exist or is not a folder (exit 5). The daemon now rejects a
  `NewSession` whose `cwd` is not absolute (`bad_intent`) instead of
  resolving it against its own working directory.

- `rex setup -tool/-model/-effort` now writes `default_spawn_tool`,
  `default_spawn_model` and `default_spawn_effort` (before, they were
  ignored). An unknown tool, model or effort exits 5 and lists the valid
  ids. The setup wizard writes the same keys, refuses a model the tool does
  not have, and keeps every other setting in an existing config.yaml.

- Descriptions no longer freeze after a burst of output, an Ollama outage or
  a state change: requests are queued per session and deferred instead of
  dropped, the agent going idle with a question is summarized, and a slow
  call no longer blocks every session for 15 s (each attempt has its own
  timeout, and only transient errors are retried).
- A finished session gets one final summary instead of keeping stale
  "Running tests…" text.
- Antigravity: the `Generating...` spinner and the text typed in its input
  box no longer show as the board's last line or reach the summary prompt.
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
