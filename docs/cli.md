# CLI reference

`rex` is the client binary. With no arguments it starts the TUI. Subcommands talk to `rex-daemon` over the Unix socket unless noted.

## NAME

rex — Runtime Executive for Agents

## SYNOPSIS

```sh
rex
rex [--help|-h|help] [topic] [-o FORMAT]
rex <command> -h
rex agent-help [-o FORMAT]
rex [--version|-v|version]
rex <command> [flags] [args...]
```

## DESCRIPTION

Commands dial the default socket unless `-socket` is set. See [paths.md](paths.md) for the default path.

## HELP

`rex --help` lists the commands by section. It is built like `agy`'s help: `Usage:` first, Title-case section headers with a colon, aligned names, no banner or rule, and one closing hint. `rex <command> -h` (or `rex help <command>`) prints that command's usage, description, flags, output formats and an example, from the same table, to stdout, and exits 0. A flag error prints the error and one `Usage:` line on stderr, not the whole list.

Help is a query like any other: `-o txt` (the default in a pipe) has `# Usage` style headers, `-o md` pastes into an issue, and `-o json` gives `{"v":1,"cmd":"help","data":{...}}` with every command (or, for `rex <command> -h`, every flag and its default). In agent mode `rex --help` prints the agent card.

`rex agent-help` (alias `rex help agent`) prints that card: at most 30 lines, about 1.2k tokens, with the line grammar, exit codes, the common commands and the interactive ones agents must not run. `rex agent-help -o json` gives the same information as data.

## FLAGS AND ARGUMENTS

Flags may come before, between or after the positional arguments: `rex wait p1 -until done` and `rex wait -until done p1` are the same. `--` ends flag parsing; everything after it is positional. A lone `-` is positional.

`rex new`, `rex reply` and `rex name` take free text. In them, an unknown `-x` after the text is an error (exit 5). Put `--` before text that starts with `-`: `rex new -- -h is the flag I want to document`, `rex name p1 -- -clear`.

`-h` or `-help` on a command prints its help (see HELP) and exits **0**.

## SELECTORS

Many commands take `<sel>`: one session reference.

| Form | Rule |
|------|------|
| Full UUID | Exact match on `id` |
| Slug | Exact match on `slug`. If the same text is also the short id or id prefix of a different session, the selector is ambiguous (exit 3) |
| Short id | Four or more hex digits, in any case; prefix match on `id` |

Exit **2** if no match. Exit **3** if multiple match: a slug shared by several sessions restored from an older daemon, or a slug that is also the short id (or id prefix) of a *different* session, such as a slug `feed` next to a session whose id starts with `feed`. The error lists each match and how it matched (`slug`, `short id`); pass the full id to pick one.

## GLOBAL EXIT STATUS

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Generic error; also `rex status` when any session is `needs_input`; `rex doctor` when any check fails |
| 2 | Selector not found |
| 3 | Ambiguous selector |
| 4 | Daemon unreachable: no socket, a stale socket, or a failed handshake. The message says `rex-daemon is not running (no socket at <path>)` and points to `rex daemon start` |
| 5 | Invalid arguments: a bad flag or value, a missing argument, or an unknown command |
| 6 | Operation refused: `rex rm` or `rex prune` without a terminal and without `-force`/`-yes`, or a declined `rex rm` or `rex prune` confirmation; also a prompt that cannot be asked (a non-text format, or stderr not a terminal) |
| 7 | `rex wait` timed out |

An unknown command exits **5** and, when a real command is within two edits, names it: `unknown command "lss"; did you mean "ls"?`. A session removed while `rex wait` runs exits **2**.

---

## OUTPUT FORMATS

Every command except `attach` and the TUI render through one output kit. Choose a format with `-o FORMAT` or `--output FORMAT`, anywhere on the command line or before the command (`rex -o json ls`).

| Format | What it is |
|--------|------------|
| `text` | The human layout: aligned columns, state markers, color for meaning, shrunk to the terminal width. Default on a terminal. |
| `txt` | Plain and stable for `grep` and `awk`: no color, no banners, fixed columns. Default in a pipe, and identical to the output rex always had there. |
| `md` | GitHub-flavored Markdown tables, for pasting into issues and PRs. |
| `json` | One versioned document: `{"v":1,"cmd":"ls","data":...}`. `v` changes only on a breaking change. |
| `agent` | One line per record, `kind key=value ...`, then `ok cmd=ls count=3`. Values that are not `[A-Za-z0-9._:/@+%-]+` are Go-quoted, so a value never breaks the line. |

Shortcuts: `--agent` is `-o agent`; `--no-color` never emits ANSI; `-json` is the **legacy** JSON (today's per-command shape: JSONL for `ls`, a flat counts object for `status`). Prefer `-o json`. Combining `-json` with `-o` or `--agent` exits 5. An unknown format, or one a command does not support, exits 5.

Precedence, highest first: flags (the last one wins), `REX_AGENT=1`, `REX_OUTPUT`, then `text` on a terminal and `txt` otherwise.

Color is on only for `text`, on a terminal, with `NO_COLOR` unset or empty, `TERM` not `dumb` and no `--no-color`. `CLICOLOR_FORCE=1` forces it in a pipe (`less -R`). Each stream decides on its own, so a redirected stderr never receives ANSI. Colors follow the `color_scheme` setting. `REX_ASCII=1` switches the glyphs to ASCII.

Errors go to stderr in the same format: styled in `text`, `rex: message` in `txt` and `md`, `error code=4 kind=daemon_unreachable cmd=ls msg="..."` in `agent`, and `{"v":1,"error":{"code":4,"kind":"daemon_unreachable",...}}` in `json` (nothing on stdout). Kinds: `error`, `not_found`, `ambiguous`, `daemon_unreachable`, `invalid_args`, `refused`, `timeout`.

**Actions** report one result record. `text` shows a `✓ …` line (and a next-command hint after `new` and `done`), `txt` keeps the line each command always printed (`7d4f<TAB>slug` for `new`, `slug: renamed to x`; nothing for `rm`, `reply`, `send`, `wait` and `complete`), `json` carries the result as data (`{"v":1,"cmd":"rename","data":{"id":…,"short_id":…,"slug":…,"old_slug":…}}`), and `agent` gives `ok cmd=rename id=7d4f slug=x old_slug=y`. Confirmation prompts go to stderr and only appear in `text` with stdin and stderr both terminals; warnings (`prune` keeping a session that changed) also go to stderr.

Every command takes these flags except `rex attach` and the bare `rex` TUI, which are interactive; before the command name they are refused (exit 5) for those two.

**Command classes** decide which formats a command supports:

| Class | Commands | Formats |
|-------|----------|---------|
| Query | `ls`, `status`, `digest`, `stats`, `fleet ls`/`show`, `config list`/`get`, `doctor`, `daemon status`, `version`, `errors`, `help`, `agent-help`, `update -check`, `setup -preview`, `uninstall -dry-run` | all five |
| Action | `new`, `rm`, `rename`, `name`, `done`, `undone`, `archive`, `complete`, `resume`, `reply`, `send`, `wait`, `prune`, `fleet set`/`unset`, `config set`/`reset`, `reload`, `daemon start`/`stop`/`restart`, `update`, `uninstall`, `setup -yes` | all five (a result record) |
| Content | `log`, `render`, `completion`, `daemon logs`, `config edit` | `text` and `txt`; `agent` behaves like `txt` (the content, no trailer); `json` and `md` exit 5 |
| Interactive | `rex` (the board), `attach`, the `setup` wizard | `text` only (`attach` also `txt`); any other format exits 5 with a hint (`rex ls`, `rex log <sel> -n 50`, `rex setup -yes`) |

**Streams.** stdout carries the result and nothing else. Errors, warnings, questions and progress go to stderr, so `rex ls | wc -l` and `$(rex new …)` stay clean. A note that qualifies the data (`digest` reading the on-disk snapshot) is part of the result: a note in `text`, `meta.source` in `json`, `source=disk` in `agent`.

**Questions.** `rm`, `prune`, `update`, `uninstall` and `errors -clear` ask on stderr, and only in `text` with stdin and stderr both terminals. Otherwise they exit 6 and name the flag that skips the question (`-force` for `rm` and `errors -clear`, `-yes` for the rest). A declined question exits 6.

**Untrusted text** (a session slug, a name, a daemon message) is flattened in every human format and Go-quoted in `agent`, so a value never carries an escape sequence or breaks a line.

---

## AGENT MODE

Built for LLM agents that drive rex from a shell. Turn it on with `--agent`, `-o agent` or `REX_AGENT=1`, or let rex do it: it switches on by itself when `CLAUDECODE=1` and stdout is not a terminal (a Claude Code Bash tool, including inside rex-spawned sessions). A human at a terminal is never affected. `REX_AGENT=0` turns it off for one run, an explicit `-o` wins, and the `cli_agent_auto` setting (Settings, Features) turns the detection off for good.

**Grammar.** One record per line: `<kind> key=value key=value …`. Keys are lowercase snake_case in a fixed order per kind. A value is bare when it matches `[A-Za-z0-9._:/@+%-]+`, otherwise Go-quoted (`strconv.Quote`), and empty is `""`. Durations are integer seconds (`idle_s=42`), times are RFC 3339 UTC, booleans are `true` or `false`.

**Shape.** A list command prints one line per record then a trailer: `ok cmd=ls count=3`. A query or action prints one `ok` line: `ok cmd=status needs_input=1 working=2 queued=0 done=3 failed=0` (the exit code is unchanged, `status` still exits 1 when a session needs input). `doctor` ends in `fail cmd=doctor fails=1 warns=2` when a check fails. An error is one line on stderr, `error code=4 kind=daemon_unreachable cmd=ls msg="rex-daemon is not running" hint="rex daemon start"`, with the normal exit code. Kinds: `error` (1), `not_found` (2), `ambiguous` (3), `daemon_unreachable` (4), `invalid_args` (5), `refused` (6), `timeout` (7).

**Implied.** No color, no spinner or progress output, and nothing ever prompts: a command that would ask needs `-force` or `-yes` and exits 6 without it. The interactive commands exit 5 with a hint. `rex --help` prints the agent card.

`rex agent-help` (alias `rex help agent`) prints that card: at most 30 lines (about 1.2k tokens) with the grammar, exit codes, the commands to read and write with, and the ones not to run. `rex agent-help -o json` gives the same as data. A snippet for your own CLAUDE.md or AGENTS.md is in [howto/agents.md](howto/agents.md); rex never writes to those files.

---

## ERROR LOG

A failed command leaves one JSON line in `~/.local/state/rex/cli-errors.log` (`$REX_LOG_DIR/cli-errors.log` when set; 0600 in a 0700 folder), so the error is still there after the terminal scrolls:

```json
{"v":1,"time":"2026-11-11T09:12:03.123Z","version":"v1","os":"darwin/arm64","pid":4242,
 "cmd":"rm","args":["rm","<sel:8 bytes>","-force"],"format":"agent","agent_auto":true,
 "tty":{"in":false,"out":false},"exit":6,"kind":"refused",
 "error":"rm: stdin is not a terminal; pass -force …","chain":["…"],"hint":"rex rm <sel> -force"}
```

What it holds: the command, flag names, the values of a short list of safe flags (`-tool -model -effort -o -until -timeout -state -after -n -since -date`), selectors as short ids or sizes, the exit code and kind, and error text scrubbed of API keys, tokens and `password=` values. What it never holds: prompts, `reply`/`send`/`name` text, `config set` values, `-cwd` or `-socket` paths, stdin, environment values or transcripts. Help requests and bare exit statuses (`status` finding a session that needs input, `doctor` failures) are not recorded.

The log is capped at 1 MiB plus one backup (`cli-errors.log.1`). Logging never changes an exit code and never prints (except one stderr line with `REX_LOG_LEVEL=debug`). Concurrent rex processes append whole lines; a rotation race can lose a few. Turn it off with the `cli_error_log` setting or `REX_ERROR_LOG=0`. Read it with [`rex errors`](#rex-errors); `rex doctor` has a `cli errors` row.

---

## rex status

Aggregate session counts.

```sh
rex status [-socket path] [-o FORMAT] [--agent] [-json]
```

Exits **1** when at least one session is `needs_input` (even with no other error).

`txt` is the line shell prompts parse (`0 awaiting input · 1 working · 2 completed`). `agent` prints `ok cmd=status needs_input=1 working=1 queued=0 done=2 failed=0`; `failed` includes crashed sessions.

---

## rex ls

List sessions.

```sh
rex ls [-socket path] [-state dir] [-tool id] [-model id] [-show-archived] [-short] [-wide] [-o FORMAT] [--agent] [-json]
```

`-show-archived` and `-short` are deprecated: they are accepted but have no effect.

`-wide` adds `STARTED` and `ENDED` columns before `NAME` (local `2006-01-02 15:04`, `-` when unset; a finished session from an older daemon shows its last event as the end). In `txt` that keeps `NAME` last; in `text` they are dropped below 100 columns; `agent` lines get `started=` and `ended=` (RFC 3339 UTC, `""` when unset) before `name`. `-o json` already carries `started_at` and `ended_at`. With the `resume` feature on, `-wide` also adds a `RESUME ID` column (the full id, `-` when unknown; `text` drops it below 140 columns; agent key `resume_id`). The `timestamps` feature (Settings → Features) switches the columns off.

The table's last column is `NAME`: the display name set with `rex name` (or `r` in the TUI), plus `✓done` when the session is marked done. It is last so scripts that cut earlier columns keep working. That layout is the `txt` format. `text` adds a state marker and a compact `IDLE` column and, on a terminal too narrow for the table, drops `TOOL` (below 60 columns) and moves `NAME` to a dim second line under each row. The `✓done` marker appears only in `text` and `txt`; `md`, `agent` and `json` carry the real name. `agent` lines carry `id state tool model effort slug idle_s name` (`idle_s` is -1 when unknown).

---

## rex new

Spawn a session.

```sh
rex new [prompt] [-socket path] [-tool id] [-model id] [-effort tier]
           [-slug name] [-cwd dir] [-fleet name] [-wait duration] [-no-attach]
```

Defaults: `-tool echo`, `-model short`, `-cwd` `$PWD`. `-cwd` must be an existing folder (exit 5 otherwise); a relative path is made absolute by the CLI. Prompt from arguments or stdin when stdin is not a TTY.

Prints `short_id` and `slug` tab-separated on success.

Without `-slug` the slug is derived from the prompt (`session` if none) and suffixed `-2`, `-3`, … if already in use. An explicit `-slug` is used as given; if another session holds it the daemon rejects the spawn (exit 5).

`-wait` (default `5s`) is how long to wait for the daemon to confirm the spawn. If it does not, `rex new` exits 1 with `daemon did not confirm the spawn within 5s; check \`rex ls\``: the session may still appear.

`-no-attach` is deprecated: it is accepted but has no effect.

---

## rex attach

Attach terminal to a session PTY.

```sh
rex attach <sel> [-socket path] [-read-only] [-no-replay]
```

Detach with **Ctrl+]**. `-read-only` does not send keyboard input. `-no-replay` skips transcript tail.

For full-screen agents (claude, codex, gemini, …) the daemon then redraws the agent's current screen, so you see it at once even if the agent is busy or the transcript tail starts mid-frame. A daemon too old for that gets **Ctrl+L** instead, which asks the agent to redraw.

---

## rex reply

Send text to a session, then press Enter (`\r`, sent as a separate write so full-screen TUIs submit instead of inserting a newline).

```sh
rex reply <sel> [text] [-socket path] [-raw]
```

Text from arguments or stdin. With no text and a terminal on stdin it exits 5 instead of waiting for end-of-file. `-raw` sends the bytes as-is, without pressing Enter.

---

## rex send

Send raw stdin to the session PTY.

```sh
rex send <sel> [-socket path]
```

Reads stdin until EOF. No positional text argument.

---

## rex log

Read transcript file.

```sh
rex log <sel> [-socket path] [-state-dir dir] [-f] [-n lines] [-bytes]
```

`-f` follows the file. `-n` shows only the last N lines (`0`, the default, shows all; a negative number exits 5). `-bytes` is a switch that prints the raw bytes, including ANSI sequences, instead of stripping them. Default state dir: `~/.local/share/rex`.

---

## rex wait

Block until a session reaches a state.

```sh
rex wait <sel> [-socket path] [-until state] [-timeout duration]
```

`-until`: `working`, `needs_input`, `done`, `failed`, or `any` (terminal states). Default `done`.

`-timeout` uses Go duration syntax (`30s`, `10m`); `0` (the default) waits without limit. Exit **7** on timeout. An unknown `-until` or a negative `-timeout` exits **5** before anything is sent. A session that is removed while waiting exits **2**.

---

## rex rm

Delete a session.

```sh
rex rm <sel> [-socket path] [-force|-yes|-y]
```

On a terminal it asks `delete <id> (<slug>)? [y/N]`; anything but `y`/`yes` exits **6**. Without a terminal on stdin (a script, a pipe, `< /dev/null`) it refuses with exit **6** unless `-force` (alias `-yes`, `-y`) is given, the same rule as `prune`.

---

## rex prune

Delete every session in the board's **Completed** column: `done`, `failed`, `crashed`, and sessions marked done. Sessions marked done whose agent is still working (or queued) are kept; the daemon clears them once they settle. Deleting kills each agent and removes its directory (`meta.json`, `transcript.log`, `hooks.log`).

```sh
rex prune [-socket path] [-dry-run] [-yes|-y]
```

Asks `clear N completed session(s)? … [y/N]` first, on stderr. A declined question exits **6**, like `rm`. `-yes` skips the question and is required when stdin or stderr is not a terminal, and in every format but `text` (exit **6** otherwise). `-dry-run` lists the sessions (`short_id`, slug, state) and deletes nothing.

Each session is checked again right before its delete. One that went back to work meanwhile (typing into a marked-done session cancels the mark) is kept and named on stderr. One already gone counts as cleared. Prints `cleared N`, then `, kept K that changed` and `(M still working kept)` when they apply. A daemon that predates acks cannot confirm deletes, so they are reported as `sent N unconfirmed`. Exits **1** if any delete fails.

The TUI equivalent is `:prune`.

---

## rex rename

Change a session's **slug** (the selector handle). Waits for the daemon to confirm and prints `<old>: renamed to <new>`.

```sh
rex rename <sel> <new-slug> [-socket path]
```

Slugs are unique: a slug held by another session (any state) is rejected — `slug "x" already in use by <short_id>`, exit 5. A slug with whitespace or control characters is rejected before contacting the daemon (exit 5). Renaming a session to its current slug succeeds.

---

## rex name

Set, clear, or print a session's **display name** — free text (max 64 characters) shown on the board instead of the slug. The slug is unchanged and stays the selector. Waits for the daemon to confirm.

```sh
rex name <sel> <text…> [-socket path]   # set
rex name -clear <sel>                   # clear (board shows slug)
rex name <sel>                          # print
```

---

## rex done / rex undone

Mark a session done **without stopping it**. It stays usable; after `-after` (default `retire_after_hours`, 168 h) the daemon kills it, removes it, and deletes its directory under `sessions/`. Typing into it first (`rex reply`, `rex send`, attach) cancels the mark. A session still working at the deadline is cleared once it settles. `rex undone` cancels. Aliases: `retire`, `unretire`.

```sh
rex done <sel> [-after 72h] [-socket path]
rex undone <sel> [-socket path]
```

`rex attach` exits with "session cleared (marked done)" and `rex wait` exits with code 2 ("session removed") when the session disappears.

---

## rex resume

Make a session live and not marked done. An exited session (done, failed or crashed) is respawned **in place** with the agent's own resume arguments (`claude --resume <id>`): same row, short id, slug, start time and transcript, which then holds both runs. A running session that was marked done loses the mark, like `rex undone`.

```sh
rex resume <sel> [-socket path] [-print] [-o FORMAT] [--agent]
rex resume -print <sel>
```

`<sel>` is a short id, slug or full id (see SELECTORS). Prints `slug: resuming`, `slug: unmarked` or `slug: already running` in `txt` (`✓ resuming 7d4f slug` in `text`); `agent` gives `ok cmd=resume id=7d4f slug=slug outcome=resuming`. `-print` sends nothing and prints only the full resume id, so `claude --resume "$(rex resume -print fix-auth)"` works in a shell. `rex ls -wide` shows the id in a `RESUME ID` column.

Exit **1** when the agent cannot be resumed (it has no resume block, no id was captured, or the folder is gone) and for `-print` on a session with no id; **2** for an unknown session; **5** when the `resume` feature is off: `off: enable in Settings → Features (rex config set resume true)`. The daemon must be current: an older one answers `daemon is older than rex` (run `rex daemon restart`).

---

## rex archive

Archive a completed session (title prefix). Like any rename or fleet change, it leaves `last_event_at` alone, so the session's duration in `rex stats` / `rex digest` and its "ago" time don't change.

```sh
rex archive <sel> [-socket path]
```

---

## rex complete

Kill a running session now and mark it done (the row stays). For the non-destructive version see `rex done`.

```sh
rex complete <sel> [-socket path]
```

---

## rex reload

Send a `Reload` request to the daemon over its socket. Reloads **tools.yaml** only. Exits 4 when no daemon answers on the socket.

```sh
rex reload
```

Does not reload `config.yaml` or `init.lua`. Silent in `txt`; `agent` gives `ok cmd=reload reloaded=true`.

---

## rex daemon

```sh
rex daemon start
rex daemon stop
rex daemon status    # exit 4 if not running
rex daemon restart
rex daemon logs [-f]
```

`stop`, `status` and `restart` act on whatever daemon answers on the socket. `stop` sends it a `Shutdown` request and returns once the daemon has removed its socket; no process is looked up by name or signaled. The PID shown is for information only and reads `unknown` when it cannot be determined (for example, two daemons on different sockets). `restart` does not start a second daemon if the first one will not stop.

`logs` reads `~/.local/state/rex/daemon.log` and prints content (`text`, `txt`, `agent`). The other subcommands take `-o` and `--agent`: `txt` keeps the line each always printed, `agent` gives `ok cmd=daemon action=status state=running socket=… pid=…`, and `status` of a stopped daemon is `fail cmd=daemon … state=not_running` (exit 4).

---

## rex config

```sh
rex config list [-o FORMAT] [--agent] [-json]
rex config get <id>...
rex config set <id> <value>...
rex config reset <id>...
rex config edit
```

`edit` opens `init.lua` in `$EDITOR`, not `config.yaml`. Keys: [settings.md](settings.md).

`list` prints `# Section` headings with `  id  value` rows in `txt` (unchanged), a `setting id=… section=… value=… default=…` line per setting in `agent`, and the rows as data in `-o json`; `-json` is the legacy JSONL. `get` prints the value (`ok cmd=config id=… value=…` in `agent`); `set` and `reset` print nothing in `txt`. `edit` takes `text`/`txt` only.

---

## rex setup

First-run wizard.

```sh
rex setup [-preview] [-yes|-y] [-tool id] [-model id] [-effort tier]
          [-auto-trust folders] [-no-shell] [-config path] [-socket path]
          [-force]
```

The wizard asks whether agents may skip the folder trust prompt in one folder; the default is no. `-auto-trust folders` sets [`auto_trust_dirs`](settings.md#auto-trust): with `-yes` it is written directly (an existing `config.yaml` needs `-force`), with `-preview` it is only printed, and otherwise the wizard starts with that answer filled in. Separate folders with `:` (`;` on Windows); `~/` is your home folder. A filesystem root, the home folder itself, a relative path or a path with `..` is refused with exit code 5. Only list folders whose code you trust.

`-tool`, `-model` and `-effort` are checked against the registry and written to `default_spawn_tool`, `default_spawn_model` and `default_spawn_effort` (the quick-spawn defaults). An unknown tool, a model the tool does not have or an effort the model does not offer exits 5 and lists the valid ids; nothing is written. A new tool without a model takes that tool's first model. Settings you do not name stay as they are, in `-yes` mode and in the wizard.

---

## rex doctor

Diagnostics. Exit **1** if any check fails.

```sh
rex doctor [-o FORMAT] [--agent] [-json] [-socket path] [-verbose]
```

Each check is a `✓` pass, `!` warn, `✗` fail or `·` info row. INFO rows are hidden in `text`/`txt`/`md` unless `-verbose`; `json` and `agent` always carry them. `agent` gives `check name=… status=… detail=…` lines then `ok cmd=doctor fails=0 warns=2`, or `fail cmd=doctor fails=1 …` when a check fails (exit 1). `-json` keeps the one-line legacy object.

With `-verbose`, an `agy version` row shows the installed Antigravity CLI version (`agy 1.5.5`). It warns below 1.5.5 (untested) or when `agy --version` fails or takes over 2 s, and is left out when `agy` is not on `PATH`.

---

## rex update

```sh
rex update [-check] [-yes|-y] [-source module|binary] [-verbose] [-o FORMAT] [--agent]
```

The plan, the `upgrade now? [Y/n]` question and the progress lines go to **stderr**; the result (`✓ rex v1 → v2`, or `ok cmd=update from=… to=…`) is the only thing on stdout. Without a terminal on stdin and stderr, or in any format but `text`, the upgrade needs `-yes` (exit **6** otherwise); a declined question exits **6**. A binary-swap install refuses with exit 6 and the command that fixes it.

---

## rex uninstall

```sh
rex uninstall [-yes|-y] [-binaries] [-wipe-state] [-purge-config]
              [-strip-profile] [-all] [-dry-run]
```

`-all` enables `-wipe-state`, `-purge-config`, and `-strip-profile`. `make uninstall` removes binaries only.

The plan (`-dry-run` prints it and exits) is a table of what would be removed; the run reports what was removed. The question goes to stderr; without a terminal on stdin and stderr, or in any format but `text`, `-yes` is required (exit **6**), and a declined question exits **6** (it was 1). `-o json` carries `{"dry_run":…,"plan":[…],"removed":[…]}`; `agent` gives `removal kind=… path=…` lines.

---

## rex digest

Daily session summary.

```sh
rex digest [-socket path] [-date YYYY-MM-DD] [-since duration] [-o FORMAT] [--agent] [-json]
```

Shows totals, a by-tool and by-model breakdown and the sessions of the window. With the daemon down it reads the on-disk snapshot and says so: a note in `text`/`txt`/`md`, `"meta":{"source":"disk"}` in `json`, `source=disk` on the `agent` trailer. `-o json` gives `{"v":1,"cmd":"digest","data":{"date","totals","by_tool","by_model","sessions"}}` with durations as integer milliseconds (`active_ms`); `agent` gives `tool`, `model` and `session` lines then `ok cmd=digest count=N date=… sessions=… active_s=…`. `-json` is the legacy indented document with string durations. `txt` (a pipe) has no banner: `label  value` lines and `# By tool` style sections.

---

## rex stats

Lifetime usage.

```sh
rex stats [-socket path] [-o FORMAT] [--agent] [-json] [-all]
```

`-o json` is the legacy `-json` document in the versioned envelope (`by_model`, `by_tool`, `totals`; `duration_ms`). `agent` gives `model` and `tool` lines then `ok cmd=stats sessions=… done=… failed=… other=…`. `txt` has no banner.

---

## rex errors

Show the CLI error log (see ERROR LOG): the failed `rex` commands, newest first.

```sh
rex errors [-n 20] [-since 24h] [-clear [-force]] [-o FORMAT] [--agent]
```

`-n` limits the count (0 = all); `-since` takes `90m`, `24h` or `7d`. `text`, `txt` and `md` show `TIME EXIT CMD ERROR`; `agent` gives `error_entry time=… exit=… kind=… cmd=… args="…" msg="…" hint="…"` lines then `ok cmd=errors count=N`; `-o json` carries the records. `-clear` deletes the log and its backup. It asks first on a terminal and needs `-force` (alias `-yes`, `-y`) otherwise (exit 6). `rex doctor` has a `cli errors` row: `none in 24h`, or a warning with the count of the last 24 hours.

---

## rex fleet

```sh
rex fleet ls [-socket path] [-o FORMAT] [--agent]
rex fleet set <sel> <fleet>
rex fleet unset <sel>
rex fleet show <fleet>
```

`ls` lists fleets with session counts (`agent`: `fleet fleet=… sessions=N` lines); `show` prints the same session table as `rex ls`; `set` and `unset` report the change like the other actions.

---

## rex render

Print a static board view (no TUI).

```sh
rex render [-socket path] [-w cols] [-h rows]
```

`render` and `log` print content: `-o text` and `txt` (and `agent`, which behaves like `txt`) work; `md` and `json` exit 5.

No daemon or a failed handshake exits **4**.

---

## rex completion

```sh
rex completion bash|zsh|fish
```

Shell script on stdout. Install via `./install.sh --shell-init`.

---

## ENVIRONMENT

See [paths.md](paths.md). Most commands accept `-socket` to override the default.

| Variable | Effect |
|----------|--------|
| `REX_OUTPUT` | Default format (`text`, `txt`, `md`, `json`, `agent`) when no flag is given |
| `REX_AGENT` | `1` turns agent mode on; `0` turns the `CLAUDECODE` auto-detection off for the run |
| `CLAUDECODE` | `1` with a piped stdout switches agent mode on (see AGENT MODE) |
| `REX_ASCII` | `1` uses ASCII glyphs in `text` |
| `REX_ERROR_LOG` | `0` stops the CLI error log |
| `REX_LOG_DIR` | Folder for the `.log` files, `cli-errors.log` included |
| `NO_COLOR` | Non-empty: no color |
| `CLICOLOR_FORCE` | Non-empty and not `0`: color even in a pipe |
| `COLUMNS` | Width for `text` when stdout is not a terminal (80 by default; 40 to 200) |

## FILES

See [paths.md](paths.md).

## SEE ALSO

- [tui.md](tui.md)
- [daemon.md](daemon.md)
- [protocol.md](protocol.md)
- [quickstart.md](quickstart.md)

## BUGS

`rex ls -short`, `rex ls -show-archived` and `rex new -no-attach` are deprecated no-ops.
