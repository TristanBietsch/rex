# CLI reference

`rex` is the client binary. With no arguments it starts the TUI. Subcommands talk to `rex-daemon` over the Unix socket unless noted.

## NAME

rex — Runtime Executive for Agents

## SYNOPSIS

```sh
rex
rex [--help|-h|help]
rex [--version|-v|version]
rex <command> [flags] [args...]
```

## DESCRIPTION

Commands dial the default socket unless `-socket` is set. See [paths.md](paths.md) for the default path.

## FLAGS AND ARGUMENTS

Flags may come before, between or after the positional arguments: `rex wait p1 -until done` and `rex wait -until done p1` are the same. `--` ends flag parsing; everything after it is positional. A lone `-` is positional.

`rex new`, `rex reply` and `rex name` take free text. In them, an unknown `-x` after the text is an error (exit 5). Put `--` before text that starts with `-`: `rex new -- -h is the flag I want to document`, `rex name p1 -- -clear`.

`-h` or `-help` on a command prints its usage and exits **0**.

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
| 6 | Operation refused: `rex rm` or `rex prune` without a terminal and without `-force`/`-yes`, or a declined `rex rm` confirmation (a declined `prune` prompt still exits 1) |
| 7 | `rex wait` timed out |

An unknown command exits **5** and, when a real command is within two edits, names it: `unknown command "lss"; did you mean "ls"?`. A session removed while `rex wait` runs exits **2**.

---

## OUTPUT FORMATS

`rex ls`, `rex status` and `rex skills` and `rex overview` (and the commands migrated after them) render through one output kit. Choose a format with `-o FORMAT` or `--output FORMAT`, anywhere on the command line or before the command (`rex -o json ls`).

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

Commands not yet on the kit refuse `-o`, `--agent` and `-json` before the command name (exit 5) instead of ignoring them.

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

Prints the daemon's one-line headline with its age (when `overview_enabled` is on and the daemon has written one), the counts, then one table per non-empty bucket: **Ready for review**, **Needs you**, **Failed**, **Working** and **Completed (last 24 h)**. The buckets are computed from the sessions, never from the headline: a session you marked done is Completed; `failed` and `crashed` are Failed; `done` is Ready for review; a Claude session that finished a turn and whose last line is not a question is Ready for review, otherwise a `needs_input` session is Needs you; the rest are Working. With the setting off the lists still print, with a note naming the setting. Exits 0.

`json` is `{"v":1,"cmd":"overview","data":{"overview":{"enabled":…,"headline":…,"generated_at":…}|null,"buckets":{"review":[ids],"needs_you":[ids],"failed":[ids],"working":[ids],"completed":[ids]}}}` with full session ids (`overview` is `null` from a daemon without one). `agent` prints an `overview headline=… generated_at=…` line when there is a headline, one `session bucket=… id=… name=… state=… status=… age_s=…` line per session, then `ok cmd=overview count=N review=… needs_you=… failed=… working=… completed=…`. The headline is model text: it is cut to one line and stripped of escape sequences, but it can be wrong.

---

## rex ls

List sessions.

```sh
rex ls [-socket path] [-state dir] [-tool id] [-model id] [-show-archived] [-short] [-o FORMAT] [--agent] [-json]
```

`-show-archived` and `-short` are deprecated: they are accepted but have no effect.

The table's last column is `NAME`: the display name set with `rex name` (or `r` in the TUI), plus `✓done` when the session is marked done. It is last so scripts that cut earlier columns keep working. That layout is the `txt` format. `text` adds a state marker and a compact `IDLE` column and, on a terminal too narrow for the table, drops `TOOL` (below 60 columns) and moves `NAME` to a dim second line under each row. The `✓done` marker appears only in `text` and `txt`; `md`, `agent` and `json` carry the real name. `agent` lines carry `id state tool model effort slug idle_s name` (`idle_s` is -1 when unknown).

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

Asks `clear N completed session(s)? … [y/N]` first. `-yes` skips the question and is required when stdin is not a terminal (exit **6** otherwise). `-dry-run` lists the sessions (`short_id`, slug, state) and deletes nothing.

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

Does not reload `config.yaml` or `init.lua`.

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

`logs` reads `~/.local/state/rex/daemon.log`.

---

## rex config

```sh
rex config list [-json]
rex config get <id>...
rex config set <id> <value>...
rex config reset <id>...
rex config edit
```

`edit` opens `init.lua` in `$EDITOR`, not `config.yaml`. Keys: [settings.md](settings.md).

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
rex doctor [-json] [-socket path] [-verbose]
```

With `-verbose`, an `agy version` row shows the installed Antigravity CLI version (`agy 1.5.5`). It warns below 1.5.5 (untested) or when `agy --version` fails or takes over 2 s, and is left out when `agy` is not on `PATH`.

---

## rex update

```sh
rex update [-check] [-yes|-y] [-source module|binary] [-verbose]
```

---

## rex uninstall

```sh
rex uninstall [-yes|-y] [-binaries] [-wipe-state] [-purge-config]
              [-strip-profile] [-all] [-dry-run]
```

`-all` enables `-wipe-state`, `-purge-config`, and `-strip-profile`. `make uninstall` removes binaries only.

---

## rex digest

Daily session summary.

```sh
rex digest [-socket path] [-date YYYY-MM-DD] [-since duration] [-json]
```

---

## rex stats

Lifetime usage.

```sh
rex stats [-socket path] [-json] [-all]
```

---

## rex fleet

```sh
rex fleet ls [-socket path]
rex fleet set <sel> <fleet>
rex fleet unset <sel>
rex fleet show <fleet>
```

---

## rex render

Print a static board view (no TUI).

```sh
rex render [-socket path] [-w cols] [-h rows]
```

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

Output: `REX_OUTPUT`, `REX_AGENT`, `REX_ASCII`, `NO_COLOR`, `CLICOLOR_FORCE`, `COLUMNS` (see OUTPUT FORMATS).

## FILES

See [paths.md](paths.md).

## SEE ALSO

- [tui.md](tui.md)
- [daemon.md](daemon.md)
- [protocol.md](protocol.md)
- [quickstart.md](quickstart.md)

## BUGS

`rex ls -short`, `rex ls -show-archived` and `rex new -no-attach` are deprecated no-ops.
