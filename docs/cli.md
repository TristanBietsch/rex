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

Flags may come before or after positional arguments, with one dash or two (`rex new "fix the bug" --tool claude` = `rex new -tool claude "fix the bug"`). `--` ends flags: everything after it is positional, even text starting with `-` (`rex reply api -- "-v is broken"`). A positional argument a command does not take exits **5**.

`rex --version` prints the version stamped at build time by `make` and the install scripts (`git describe --tags --always --dirty`), else the Go module version, else `dev`.

## SELECTORS

Many commands take `<sel>`: one session reference.

| Form | Rule |
|------|------|
| Full UUID | Exact match on `id` |
| Slug | Exact match on `slug` (checked before the aliases and short id) |
| `@needs`, `@working`, `@done` | Every session in state `needs_input`, `working`, or `done` |
| Short id | Four or more hex digits; prefix match on `id` |

Exit **2** if no match. Exit **3** if multiple match — including a slug shared by several sessions restored from an older daemon, and an alias when more than one session is in that state (the error lists their short ids).

A session whose slug is literally `@needs` (or `@working`, `@done`) wins over the alias.

## GLOBAL EXIT STATUS

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Generic error; also `rex status` when any session is `needs_input`; `rex doctor` when any check fails; `rex uninstall` when a removal fails |
| 2 | Selector not found |
| 3 | Ambiguous selector |
| 4 | Daemon unreachable (connect or `Hello` handshake failed) |
| 5 | Invalid arguments |
| 6 | Operation refused |
| 7 | `rex wait` timed out |

Unknown commands print to stderr and exit **5**.

---

## rex status

Aggregate session counts.

```sh
rex status [-socket path] [-json]
```

Exits **1** when at least one session is `needs_input` (even with no other error).

---

## rex ls

List sessions.

```sh
rex ls [-socket path] [-state state] [-tool id] [-model id] [-show-archived] [-json]
```

`-state`, `-tool`, and `-model` keep only sessions whose value matches exactly. States: `queued`, `working`, `needs_input`, `done`, `failed`, `crashed`.

Archived sessions (`rex archive`) are hidden; `-show-archived` lists them too.

The table's last column is `NAME`: the display name set with `rex name` (or `r` in the TUI), plus `✓done` when the session is marked done. It is last so scripts that cut earlier columns keep working.

---

## rex new

Spawn a session.

```sh
rex new [prompt] [-socket path] [-tool id] [-model id] [-effort tier]
           [-slug name] [-cwd dir] [-fleet name]
```

Defaults: `-tool echo`, `-model short`, `-cwd` `$PWD`. Prompt from arguments or stdin when stdin is not a TTY.

Prints the new session's `short_id` and `slug`, tab-separated. It waits up to 5 s for the daemon to announce a session with the slug it asked for; sessions other clients spawn meanwhile are skipped.

Without `-slug` the slug is derived from the prompt (`session` if none) and suffixed `-2`, `-3`, … if already in use. An explicit `-slug` is used as given; if another session holds it the daemon rejects the spawn (exit 5).

---

## rex attach

Attach terminal to a session PTY.

```sh
rex attach <sel> [-socket path] [-read-only] [-no-replay]
```

Detach with **Ctrl+]**, also with `-read-only`. `-read-only` sends nothing to the session: no keyboard input, and no redraw kick (the Ctrl+L sent 150 ms after attaching to `claude`, `codex`, `gemini`, `grok`, `deepseek`, or `kimi` so they repaint). A read-only attach stays attached if its stdin ends. `-no-replay` skips the transcript tail.

If the session is deleted or cleared while attached, `rex attach` exits **1** ("session deleted" or "session cleared (marked done)").

---

## rex reply

Send text to a session, then press Enter (`\r`, sent as a separate write so full-screen TUIs submit instead of inserting a newline).

```sh
rex reply <sel> [text] [-socket path] [-raw]
```

Text from the arguments (joined with spaces) or stdin. Text starting with `-` goes after `--`. The daemon drops control characters other than newline and tab (so no escape sequences or early carriage return) before typing it. `-raw` sends the bytes as-is, without pressing Enter.

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

`-f` follows the file. `-n` shows only the last N lines (default all). Output has CSI escape sequences (colors, cursor movement) stripped, while following too; `-bytes` (a switch, takes no value) prints the raw bytes instead. Default state dir: `~/.local/share/rex`.

---

## rex wait

Block until a session reaches a state.

```sh
rex wait <sel> [-socket path] [-until state] [-timeout duration]
```

`-until`: `working`, `needs_input`, `done`, `failed`, `crashed`, or `any` (`done`, `failed`, or `crashed`). Default `done`. Any other value exits **5** before contacting the daemon.

`-timeout` uses Go duration syntax (`30s`, `10m`). Exit **7** on timeout. Exit **2** if the session is removed while waiting.

---

## rex rm

Delete a session.

```sh
rex rm <sel> [-socket path] [-force]
```

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

Archive a completed session: its title gets the prefix `[archived] `, and `rex ls` hides it unless `-show-archived`. The TUI board still shows it. Like any rename or fleet change, it leaves `last_event_at` alone, so the session's duration in `rex stats` / `rex digest` and its "ago" time don't change.

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

Send SIGHUP to the daemon. Reloads **tools.yaml** only.

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

`logs` reads `~/.local/state/rex/daemon.log`.

---

## rex config

```sh
rex config list [-json]
rex config get <id>
rex config set <id> <value>
rex config reset <id>
rex config edit
```

`get` and `reset` take exactly one setting id, `set` exactly one id and one value (quote a value with spaces); anything else exits **5**.

`edit` opens the Lua config at `lua_config_path` (default `~/.config/rex/init.lua`, the file the daemon loads) in `$EDITOR`, not `config.yaml`. It creates the file's directory if needed. `$EDITOR` may carry arguments (`code -w`); unset, it is `vi` (`notepad` on Windows). Keys: [settings.md](settings.md).

---

## rex setup

First-run wizard.

```sh
rex setup [-preview] [-yes|-y] [-tool id] [-model id] [-effort tier]
          [-no-shell] [-config path] [-socket path] [-force]
```

---

## rex doctor

Diagnostics. Exit **1** if any check fails.

```sh
rex doctor [-json] [-socket path] [-verbose]
```

Checks: `rex` on `PATH`; `rex-daemon` where `rex daemon start` looks for it (next to `rex` first, then `PATH`); the daemon socket; the config, state, and sessions directories; `config.yaml` parsing; agent CLIs on `PATH`. It only looks and never creates anything: a missing directory is an INFO row (rex creates it on first use). INFO rows show with `-verbose` or `-json`.

---

## rex update

```sh
rex update [-check] [-yes|-y] [-source path] [-verbose]
```

Without `-source`, `rex update` runs `go install`, so it only upgrades a `rex` installed under `$GOPATH/bin` or `~/go/bin`. Any other install (`install.sh`, `make install`) is refused with exit **6** before the prompt and before a running daemon is stopped; re-run the install script instead. Declining the prompt also exits **6**.

`-source` names a source checkout: runs `git pull` there, then its `install.sh` (`install.ps1` on Windows).

---

## rex uninstall

```sh
rex uninstall [-yes|-y] [-binaries] [-wipe-state] [-purge-config]
              [-strip-profile] [-all] [-dry-run]
```

`-all` enables `-wipe-state`, `-purge-config`, and `-strip-profile`. `make uninstall` removes binaries only.

Each failed removal is printed and the command exits **1** after trying the rest. `-strip-profile` leaves a file whose `# BEGIN REX` block has no `# END REX` unchanged (remove the block by hand); that counts as a failure. Declining the confirmation exits **1**. `-binaries` for a `rex` or `rex-daemon` outside your home directory is refused (exit **6**).

---

## rex digest

Daily session summary.

```sh
rex digest [-socket path] [-date YYYY-MM-DD|yesterday] [-since Nd|Nh] [-json]
```

Default: today. `-since` takes whole days or hours only: `7d` covers today and the 6 days before it, `24h` the last 24 hours; other units exit **5**. `-date` and `-since` are mutually exclusive (exit **5**).

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
rex fleet set <sel> <fleet> [-socket path]
rex fleet unset <sel> [-socket path]
rex fleet show <fleet> [-socket path]
```

`set` and `unset` wait up to 3 s for the daemon to confirm. A rejection is printed: exit **5** for a bad request, **1** otherwise or when no confirmation arrives.

---

## rex render

Print a static board view (no TUI).

```sh
rex render [-socket path] [-w cols] [-h rows]
```

Default size 120×30. `-w` and `-h` must be positive (exit **5**). Exit **4** if the daemon is unreachable.

---

## rex completion

```sh
rex completion bash|zsh|fish
```

Shell script on stdout. Install via `./install.sh --shell-init`.

---

## ENVIRONMENT

See [paths.md](paths.md). Most commands accept `-socket` to override the default.

## FILES

See [paths.md](paths.md).

## SEE ALSO

- [tui.md](tui.md)
- [daemon.md](daemon.md)
- [protocol.md](protocol.md)
- [quickstart.md](quickstart.md)

## BUGS

`rex log` strips only CSI sequences (colors, cursor movement). Other escapes, such as OSC window titles, pass through to the terminal; `-bytes` passes everything.
