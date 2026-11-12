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
| 6 | Operation refused: `rex rm` or `rex prune` without a terminal and without `-force`/`-yes`, or a declined `rex rm` or `rex prune` confirmation; `rex undo` / `rex redo` without a terminal and without `-yes`, a declined confirmation, or a daemon refusal (session working, another live session in the folder, nothing to undo or redo, folder not in a git repo); also a prompt that cannot be asked (a non-text format, or stderr not a terminal) |
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

Color is on only for `text`, on a terminal, with `NO_COLOR` unset or empty, `TERM` not `dumb` and no `--no-color`. `CLICOLOR_FORCE=1` forces it in a pipe (`less -R`). Each stream decides on its own, so a redirected stderr never receives ANSI. Colors follow the `theme` setting. `REX_ASCII=1` switches the glyphs to ASCII.

Errors go to stderr in the same format: styled in `text`, `rex: message` in `txt` and `md`, `error code=4 kind=daemon_unreachable cmd=ls msg="..."` in `agent`, and `{"v":1,"error":{"code":4,"kind":"daemon_unreachable",...}}` in `json` (nothing on stdout). Kinds: `error`, `not_found`, `ambiguous`, `daemon_unreachable`, `invalid_args`, `refused`, `timeout`.

**Actions** report one result record. `text` shows a `✓ …` line (and a next-command hint after `new` and `done`), `txt` keeps the line each command always printed (`7d4f<TAB>slug` for `new`, `slug: renamed to x`; nothing for `rm`, `reply`, `send`, `wait` and `complete`), `json` carries the result as data (`{"v":1,"cmd":"rename","data":{"id":…,"short_id":…,"slug":…,"old_slug":…}}`), and `agent` gives `ok cmd=rename id=7d4f slug=x old_slug=y`. Confirmation prompts go to stderr and only appear in `text` with stdin and stderr both terminals; warnings (`prune` keeping a session that changed) also go to stderr.

Every command takes these flags except `rex attach` and the bare `rex` TUI, which are interactive; before the command name they are refused (exit 5) for those two.

**Command classes** decide which formats a command supports:

| Class | Commands | Formats |
|-------|----------|---------|
| Query | `ls`, `status`, `digest`, `stats`, `fleet ls`/`show`, `config list`/`get`, `theme list`/`show`, `doctor`, `daemon status`, `version`, `errors`, `help`, `agent-help`, `update -check`, `setup -preview`, `uninstall -dry-run` | all five |
| Action | `new`, `rm`, `rename`, `name`, `done`, `undone`, `archive`, `complete`, `resume`, `reply`, `send`, `wait`, `prune`, `fleet set`/`unset`, `config set`/`reset`, `theme new`, `reload`, `daemon start`/`stop`/`restart`, `update`, `uninstall`, `setup -yes` | all five (a result record) |
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

A failed command leaves one JSON line in `~/.local/state/rex/cli-errors.log` (`$REX_LOG_DIR/cli-errors.log` when set; 0600 in a 0700 folder), so the failure is still known after the terminal scrolls. The log is **structure only**: no text a person typed and no error message is ever stored, because free text cannot be scrubbed reliably.

```json
{"v":1,"time":"2026-11-11T09:12:03.123Z","version":"v1","os":"darwin/arm64","pid":4242,
 "cmd":"fleet","sub":"set","flags":["-socket"],"arg_count":4,"unknown_args":0,
 "format":"agent","agent_auto":true,"tty":{"in":false,"out":false},
 "exit":4,"kind":"daemon_unreachable","code":"daemon_unreachable",
 "chain":["core.ExitError","*net.OpError","*os.SyscallError","syscall.Errno"]}
```

Exactly what a record holds:

| Field | Content |
|-------|---------|
| `v`, `time`, `version`, `os`, `pid` | Record version, UTC time, rex version, `GOOS/GOARCH`, process id |
| `cmd` | The command, only when rex knows the name; `?` otherwise (so a prompt typed as a command is not kept) |
| `sub` | The subcommand, only from rex's fixed lists (`fleet set`, `config get`, `daemon status` …) |
| `flags` | **Names** of flags the command defines (and the global `-o`, `--agent`, `--no-color`, `-json`), never values |
| `arg_count`, `unknown_args` | How many arguments there were, and how many dash arguments named no flag (a prompt that starts with `-` is counted, not logged) |
| `format`, `agent_auto`, `tty` | Output format, whether `CLAUDECODE` chose agent mode, whether stdin and stdout were terminals |
| `exit`, `kind` | Exit code and its kind (`refused`, `daemon_unreachable` …) |
| `code` | A stable error name from a fixed list (`daemon_unreachable`, `handshake_failed`, `selector_not_found`, `selector_ambiguous`, `unknown_command`, `invalid_flag`, `feature_off`, `needs_confirmation`), when the error carries one |
| `chain` | The Go **type** of the error and of each cause (`*net.OpError`, `*fs.PathError` …), never their text |

Never stored: error messages, hints, argument or flag values, selectors, prompts, `reply`/`send`/`name` text, `config set` values, folders, socket paths, file names, stdin, environment values, transcripts. Help requests and bare exit statuses (`status` finding a session that needs input, `doctor` failures) are not recorded.

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

## rex overview

What needs you, what is ready for review, what failed, what is working and what finished.

```sh
rex overview [-socket path] [-o FORMAT] [--agent]
```

Prints the daemon's one-line headline with its age (when `overview_enabled` is on and the daemon has written one), the counts, then one table per non-empty bucket: **Review**, **Ready**, **Failed**, **Working** and **Completed (last 24 h)**. The buckets are computed from the sessions, never from the headline: a session you marked done is Completed; `failed` and `crashed` are Failed; `done` is Ready for review; a Claude session that finished a turn and whose last line is not a question is Ready for review, otherwise a `needs_input` session is Ready; the rest are Working. With the setting off the lists still print, with a note naming the setting. Exits 0.

`json` is `{"v":1,"cmd":"overview","data":{"overview":{"enabled":…,"headline":…,"generated_at":…}|null,"buckets":{"review":[ids],"needs_you":[ids],"failed":[ids],"working":[ids],"completed":[ids]}}}` with full session ids (`overview` is `null` from a daemon without one). `agent` prints an `overview headline=… generated_at=…` line when there is a headline, one `session bucket=… id=… name=… state=… status=… age_s=…` line per session, then `ok cmd=overview count=N review=… needs_you=… failed=… working=… completed=…`. The headline is model text: it is cut to one line and stripped of escape sequences, but it can be wrong.

---

## rex export

Write one session as a readable document.

```sh
rex export <sel> [-o json|yaml|md|txt] [--out FILE] [--force] [--max-frames N] [--from-agent-log]
```

Replays the session's transcript through the terminal emulator (full-screen agents) or strips it (line tools) and writes the metadata plus clean text as `json` (default, `version: 1`), `yaml` (the same document), `md` or `txt`. Output goes to stdout, or to `--out FILE` (mode 0600; refuses to replace a file without `--force`, exit 6; the format is taken from the extension when `-o` is absent). `--max-frames N` keeps the last N frames. `--from-agent-log` reads Claude Code's own log for the session's resume id instead; a missing log falls back to frames with a notice. A running session is read up to its current end. Needs the `session_export` feature (exit 5 when off). Exports can hold secrets. Details: [export.md](export.md).

---

## rex ws

Workspaces: mark a project folder with a `.rex/` identity (id, name, color). See [workspaces.md](workspaces.md).

```sh
rex ws init [path] [--name N]
rex ws id [path]
rex ws ls
rex ws rename <id|path> <name>
rex ws color <id|path> <#RRGGBB|1-7>
rex ws open [path]
rex here
```

`init` creates `<path>/.rex/workspace.json` (default: the current folder), registers it in `~/.local/share/rex/workspaces.json` and adds `/.rex/` and `/.rex/audit.jsonl` to the repository's `.git/info/exclude`; on a folder that already is a workspace it keeps the id. `$HOME` and `/` are refused (exit 5). `id` prints the workspace around a folder, `ls` the known ones (with `(missing)` when the folder is gone). `<id|path>` is an 8-hex id or any folder inside the workspace. `color` takes `#RRGGBB` or a swatch number from the colorblind-safe palette (1-7). All take `-o text|txt|md|json|agent`.

`open [path]` and `rex here` open the board for the workspace around a folder (the current one by default): only its sessions, its id and name in the header, its color on the border; see [workspaces.md](workspaces.md#sessions-and-the-workspace-board). They need a terminal (no `-o`, `--agent` or pipe) and exit **5** with ``no .rex/ here — run `rex ws init` `` when there is no workspace above the folder. `rex here` takes no arguments.

`json` is `{"v":1,"cmd":"ws init","data":{"id","name","color","root","created","git_excluded"}}` (`ws id`, `rename` and `color` carry the same fields without the last two); `ws ls` data is an array of `{"id","name","color","root","last_seen","present"}`.

Exit **5** with `off: enable in Settings → Features (rex config set workspaces true)` while `workspaces` is off (nothing is read or written), and for `init` on `$HOME` or `/`. Exit **2** when no workspace matches, **6** for an unsafe or invalid `.rex/` (a symlink, writable by others, owned by another user, or an oversized or malformed `workspace.json`).

---

## rex audit

One line per finished session, from the audit trail. See [workspaces.md](workspaces.md#audit-trail).

```sh
rex audit [--ws <id|path> | --all] [--since 7d] [--tool T] [--state S] [-o FORMAT] [--agent]
```

Reads the files itself (the daemon only writes them), newest first. With no scope flag it reads the workspace around the current folder when `workspaces` is on, else the global trail (`~/.local/share/rex/audit.jsonl`). `--ws` reads one workspace's trail and needs `workspaces` on; `--all` reads the global trail and every known workspace's. `--since` is `7d`, `12h`, `90m` or a date; `--state` is `done`, `failed` or `crashed`. A trail that cannot be read (a symlink, an unsafe `.rex/`) is skipped with a warning line.

`json` is `{"v":1,"cmd":"audit","data":[{"ts","workspace_id","session_id","short_id","name","tool","model","prompt_first_line","prompt","state","started_at","ended_at","duration_s","resume_id","description"}]}`; `prompt` appears only when `audit_full_prompt` was on for that session. `agent` prints one `audit …` line per record, then `ok cmd=audit count=N`.

Exit **5** with `off: enable in Settings → Features (rex config set audit_trail true)` while `audit_trail` is off, for a bad `--since` or `--state`, and for `--ws` while `workspaces` is off. Exit **2** for an unknown `--ws`.

---

## rex usage

What the agents used: tokens, cost and plan quota, per agent, per model and in total.

```sh
rex usage [-range today|7d|30d] [-tool ID] [-by tool|model] [-socket path] [-o FORMAT] [--agent]
```

Asks the daemon, which counts each session once from its usage ledger (`~/.local/share/rex/usage.jsonl`, see [paths.md](paths.md)) overlaid with the live sessions, so a session still running counts as it is now. A session belongs to the range its latest ledger line falls in. `-range` is `today` (since local midnight, the default), `7d` or `30d`. `-tool` limits it to one tool id. Default rows are one per tool with its models under it; `-by tool` shows only the tool rows and `-by model` one flat row per tool and model. `-by` changes the human rows only: `json` always carries the whole report.

Columns: sessions, tokens and cost. **Tokens are rex's own estimate** (`output_bytes / 4`) and show a `~` in front (`~1.2M`); **cost** is what Claude Code reports for the session through its status line, `—` when the tool reported none (every tool but Claude today, and Claude when the tap is off, see `usage_statusline_tap` in [settings.md](settings.md)). A **Quota** table follows when a tool reports plan limits (Claude, through the status-line tap): window (`5h`, `week` or a spend period), percent used (green under 60, yellow under 85, red from there), when it resets, and `(as of HH:MM)` when the reading is older than 15 minutes. A plan without rate limits (API-key billing) shows cost and no quota table. Grok's `grok usage` is not read yet.

`json` is `{"v":1,"cmd":"usage","data":{"range","since","tool","total":{"sessions","tokens","tokens_estimated","cost_usd"},"tools":[{"tool",…row,"models":[{"model",…row}]}],"quotas":[{"tool","window","used_pct","resets_at","used_usd","limit_usd","at","stale"}]}}`; `cost_usd` is absent when no counted session reported one. `agent` prints one `usage level=tool|model tool=… model=… sessions=… tokens=… estimated=… cost_usd=…` line per row (`cost_usd=none` when unknown), one `quota tool=… window=… used=… resets_at=… stale=…` line per window, then `ok cmd=usage count=N sessions=… tokens=… cost_usd=…`.

Exit **5** with `off: enable in Settings → Features (rex config set usage true)` while the `usage` setting is off (checked before any connection), and for a bad `-range` or `-by`. Exit **4** when the daemon is not running. Against a `rex-daemon` older than this command it exits **6** with ``run `rex daemon restart` ``.

---

## rex loop

```
rex loop ls [-socket path] [-o FORMAT]
rex loop start <name> [-cwd DIR]
rex loop pause|resume|stop <loop-id>
rex loop log <loop-id> [-n N]
rex loop new <name>
```

Repeat loops: the daemon re-sends a prompt each time the agent finishes a
turn, until a check command you wrote exits 0 or a cap is reached. See
[loops.md](loops.md); the board has loop cards and keys too (`p`, `x`, `L`). Every subcommand exits **5** with `off: enable in
Settings → Features (rex config set loops true)` while the `loops` setting is
off, before any file is read or any connection is tried.

- `ls` lists the files in `~/.config/rex/loops` (an invalid one shows why)
  and the daemon's loops with state, turn, and why a loop is paused or ended.
  Without a daemon only the files show.
- `start <name>` starts `~/.config/rex/loops/<name>.yaml`. The folder is
  `-cwd`, else the file's `cwd`, else the current folder; it must be absolute
  without `..`. The CLI reads the file too, so a typo or an unsafe file fails
  before the daemon is asked. Exit 2 for a missing file, 5 for an invalid
  one.
- `pause`, `resume`, `stop` take a loop id (`loop-1a2b3c4d`, or just the 8
  hex digits). `stop` ends the loop for good and leaves the session running.
- `log` prints the last `-n` lines (default 200) of the loop's log: each
  turn end, check command, exit code and captured output, and why it ended.
- `new <name>` writes a commented starter file (0600, never over an
  existing one) and opens `$EDITOR` when stdin is a terminal.

`json` is `{"v":1,"cmd":"loop","data":...}` where `data` is the loop summary
(`start`, `pause`, `resume`, `stop`), `{"dir","files":[{"name","kind","tool",
"model","check","marker","max_iterations","error"}],"loops":[...],"daemon"}`
(`ls`), `{"loop_id","lines","log"}` (`log`) or `{"name","path"}` (`new`).
A loop summary is `id`, `name`, `kind`, `state`, `pause_reason`,
`stop_reason`, `session_id`, `cwd`, `iteration`, `max_iterations`,
`last_check_exit`, `started_at`, `updated_at`, `notice`.

Exit **4** when the daemon is not running. Exit **6** when the daemon
refuses: `too many loops`, a loop in the wrong state, an unknown loop, and
against a `rex-daemon` older than loops, ``run `rex daemon restart` ``.

---

## rex ls

List sessions.

```sh
rex ls [-socket path] [-state dir] [-tool id] [-model id] [-show-archived] [-short] [-wide] [-o FORMAT] [--agent] [-json]
```

`-show-archived` and `-short` are deprecated: they are accepted but have no effect.

`-wide` adds `STARTED` and `ENDED` columns before `NAME` (local `2006-01-02 15:04`, `-` when unset; a finished session from an older daemon shows its last event as the end). In `txt` that keeps `NAME` last; in `text` they are dropped below 100 columns; `agent` lines get `started=` and `ended=` (RFC 3339 UTC, `""` when unset) before `name`. `-o json` already carries `started_at` and `ended_at`. With the `resume` feature on, `-wide` also adds a `RESUME ID` column (the full id, `-` when unknown; `text` drops it below 140 columns; agent key `resume_id`). The `timestamps` feature (Settings → Features) switches the columns off.

The table's last column is `NAME`: the display name set with `rex name` (or `r` in the TUI), plus `✓done` when the session is marked done. It is last so scripts that cut earlier columns keep working. That layout is the `txt` format. `text` adds a state marker and a compact `IDLE` column and, on a terminal too narrow for the table, drops `TOOL` (below 60 columns) and moves `NAME` to a dim second line under each row. The `✓done` marker appears only in `text` and `txt`; `md`, `agent` and `json` carry the real name. `agent` lines carry `id state tool model effort slug idle_s name` (`idle_s` counts from `last_event_at`, or `started_at` when a daemon that predates creation stamping sends none; -1 when both are unknown).

A working session blocked on a tool (a shell command, subagent or web call, for 3 s or more; Claude Code) shows `waiting (shell)`, `waiting (subagent)` or `waiting (web)` in the `STATE` column of `text` and `md`, and the header counts it as `N waiting`. `txt` and the agent `state=` key keep `working`, so scripts do not change; `json` carries `waiting_on` and `waiting_since`. `waiting_section` off shows plain `working`. `rex status` is unchanged: a waiting session counts as working and does not make it exit 1.

---

## rex new

Spawn a session.

```sh
rex new [prompt] [-socket path] [-tool id] [-model id] [-effort tier]
           [-slug name] [-cwd dir] [-fleet name] [-name text] [-skill name]...
           [-wait duration] [-no-attach]
```

Defaults: `-tool echo`, `-model short`, `-cwd` `$PWD`. `-cwd` must be an existing folder (exit 5 otherwise); a relative path is made absolute by the CLI. Prompt from arguments or stdin when stdin is not a TTY.

Prints `short_id` and `slug` tab-separated on success.

Without `-slug` the slug is derived from the prompt (`session` if none) and suffixed `-2`, `-3`, … if already in use. An explicit `-slug` is used as given; if another session holds it the daemon rejects the spawn (exit 5).

`-name` sets the session's display name (see `rex name`; whitespace is collapsed, at most 64 characters). With no prompt and no `-slug`, the slug is derived from the name.

`-skill name` loads a skill from the library at `~/.config/rex/skills` into this session only; repeat it for more (at most 16). The daemon copies the skills into the session's folder and starts Claude Code with `--plugin-dir`, so nothing is written to `~/.claude`. List the names with `rex skills`; see [howto/skills.md](howto/skills.md). Exit 5 when a name is invalid or not in the library, when the tool cannot load skills per session (Antigravity cannot), or when the `spawn_skills` setting is off (`off: enable in Settings → Features (rex config set spawn_skills true)`). Against a `rex-daemon` older than these flags, `rex new` still spawns and prints `warning: this rex-daemon ignored skills; run \`rex daemon restart\`` on stderr.

`-wait` (default `5s`) is how long to wait for the daemon to confirm the spawn. If it does not, `rex new` exits 1 with `daemon did not confirm the spawn within 5s; check \`rex ls\``: the session may still appear.

`-no-attach` is deprecated: it is accepted but has no effect.

---

## rex skills

List the skill library, `~/.config/rex/skills/<name>/SKILL.md`: the names `rex new -skill` accepts.

```sh
rex skills [-o FORMAT] [--agent]
```

Columns: `name`, `description` (from the `description` in the SKILL.md frontmatter). Folders that cannot be used (a symlink, a name that is not lowercase letters, digits and hyphens, no SKILL.md) are skipped and shown as warnings. Needs no daemon. Exits 5 with the Features message when `spawn_skills` is off.

---

## rex attach

Attach terminal to a session PTY.

```sh
rex attach <sel> [-socket path] [-read-only] [-no-replay]
```

Detach with **Ctrl+]**. `-read-only` does not send keyboard input. `-no-replay` skips transcript tail. There is no banner row here: the agent draws on your terminal directly; the in-TUI popup has one ([tui.md](tui.md#banner)).

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

## rex undo / rex redo

Put a session's folder back the way it was before the agent's last turn, from the [turn snapshots](snapshots.md). Needs the `turn_snapshots` feature (off by default); without it both commands exit **5** with `off: enable in Settings → Features (rex config set turn_snapshots true)`.

```sh
rex undo <sel> [-turns N] [-force] [-yes|-y] [-socket path] [-o text|txt|md|json|agent]
rex redo <sel> [-force] [-yes|-y] [-socket path] [-o ...]
```

- `-turns N` (undo only, default 1, at most 1000) goes back N turns. Repeating `rex undo` walks further back.
- The command first asks the daemon what it would do (a dry run), so a refusal comes before any question. Then it asks `undo <slug> (N turn(s)): restore X file(s), delete Y file(s)`, adds `shell side effects (installs, databases, files outside the folder) are not undone`, and waits for `y`/`yes`. Anything else exits **6** with nothing changed.
- Without a terminal on stdin it exits **6** unless `-yes` (alias `-y`) is given, the same rule as `rm` and `prune`.
- The daemon refuses (exit **6**) while the session is working, and while another live session shares the folder unless `-force`. It also refuses a folder outside a git repo ("snapshots need a git repo"), a session with no recorded turn ("nothing to undo"), a turn whose capture failed, and a `redo` with nothing to redo.
- The daemon also refuses (exit **6**, nothing changed) when the restore would overwrite or remove content that no snapshot holds: a nested repo or `git clone` where a file must be written, a file over 2 MiB, an ignored folder, a symlink. The message lists the paths; move them away and run it again. `-force` does not override this. Warnings from the daemon (the folder changed while it ran) are shown after the file lists and are in `-o json` as `warnings`.
- Before it restores, the daemon captures the current folder (your own edits included), which `rex redo` brings back. Redo history is dropped when the session starts another turn.
- The agent is not told; the output suggests telling it yourself.
- `-o json` data: `{session, op, turns, restored[], deleted[], safety, warnings[]}`; paths are relative to the session's folder. The text views list at most 20 files per list.

Exit codes: 2 selector not found, 5 usage error or feature off, 6 refused, 1 any other failure. An older daemon answers `bad_intent` and the command says to run `rex daemon restart`.

The TUI `:undo` / `:redo` dialog is not available yet.

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

`<sel>` is a short id, slug or full id (see SELECTORS). Prints `slug: resuming`, `slug: unmarked` or `slug: already running` in `txt` (`✓ resuming 7d4f slug` in `text`); `agent` gives `ok cmd=resume id=7d4f slug=slug outcome=resuming`. `-print` sends nothing and prints only the full resume id (the bare id even where `CLAUDECODE` switched agent mode on by itself; an explicit `-o` or `--agent` decides), so `claude --resume "$(rex resume -print fix-auth)"` works in a shell. `rex ls -wide` shows the id in a `RESUME ID` column.

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
rex config edit [-lua]
rex config lua-export [-o FORMAT]
```

`edit` opens `init.lua` in `$EDITOR`, not `config.yaml`; `edit -lua` opens `~/.config/rex/config.lua` ([howto/lua-config.md](howto/lua-config.md)), creating it from a commented template the first time (0600 in a 0700 folder, never over an existing file). `lua-export` prints the values `config.yaml` holds as `rex.set(…)` lines, a starter `config.lua` (strings quoted for Lua, non-ASCII bytes as `\ddd`); it never writes a file. Both exit 5 with `off: enable in Settings → Features (rex config set lua_config true)` while `lua_config` is off. Keys: [settings.md](settings.md).

`list` prints `# Section` headings with `  id  value` rows in `txt` (unchanged), an `ID VALUE SOURCE` table per section in `text` and `md` (source `default`, `config.yaml`, or `lua config.lua:12`), a `setting id=… section=… value=… default=… source=… set_at=…` line per setting in `agent`, and the rows as data with `source` (`default`, `file`, `lua`) and `set_at` (Lua only) in `-o json`; `-json` is the legacy JSONL, unchanged. `get` prints the value (with `(lua config.lua:12)` after it in `text` when `config.lua` sets it; the bare value in `txt`), `ok cmd=config id=… value=… source=…` (plus `set_at=…` for a Lua value) in `agent`, and `source`/`set_at` in `-o json`; `set` and `reset` print nothing in `txt`. `set` and `reset` on a setting `config.lua` sets exit 5 and name the file and line. When `config.lua` fails or refuses something, `list` and `get` print one warning on stderr; stdout keeps its shape. `edit` takes `text`/`txt` only.

---

## rex theme

```sh
rex theme list [-o FORMAT] [--agent]
rex theme show <name> [-o FORMAT] [--agent]
rex theme new <name> [-from <builtin>] [-force] [-o FORMAT] [--agent]
```

`list` shows the six built-in themes, the files in `~/.config/rex/themes` (source `file`) and the themes `config.lua` defines (source `lua`; one beats a file of the same name, with a warning), marks the current one, and reports any file it skipped and why, and a `config.lua` that failed. `show` prints a theme's colors, glyphs and a sample line; in `-o json` it carries `colors`, `fleet`, `glyphs`, `warnings` and, for a file, its `path`. `new` writes a commented skeleton that extends `-from` (default `default`) to `~/.config/rex/themes/<name>.yaml`, mode 0600 in a 0700 folder; use it with `rex config set theme <name>`. Agent mode gives a `theme name=… source=… current=…` line per theme, a `color token=… value=…` and `glyph token=… value=…` line per token, and `ok cmd=theme name=… path=… from=…` for `new`. The file format is in [themes.md](themes.md).

Exit **5** for a missing or extra argument, an unknown theme in `show` or `-from`, and a name that is invalid or a built-in's. Exit **6** when `new` would overwrite a file and `-force` is not given. `new -force` replaces the file (a symlink at that path is replaced, never followed).

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

`-n` limits the count (0 = all); `-since` takes `90m`, `24h` or `7d`. `text`, `txt` and `md` show `TIME EXIT CMD KIND CODE FLAGS CHAIN`; `agent` gives `error_entry time=… exit=… cmd=… sub=… kind=… code=… flags="…" chain="…"` lines then `ok cmd=errors count=N`; `-o json` carries the records. There is no message to show: the log stores none. `-clear` deletes the log and its backup. It asks first on a terminal and needs `-force` (alias `-yes`, `-y`) otherwise (exit 6). `rex doctor` has a `cli errors` row: `none in 24h`, or a warning with the count of the last 24 hours.

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
