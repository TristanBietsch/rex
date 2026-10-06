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

## SELECTORS

Many commands take `<sel>`: one session reference.

| Form | Rule |
|------|------|
| Full UUID | Exact match on `id` |
| Slug | Exact match on `slug` |
| Short id | Four or more hex digits; prefix match on `id` |

Exit **2** if no match. Exit **3** if multiple match.

The interactive help text mentions `@needs`, `@working`, and `@done`. Those aliases are **not** implemented. Use `rex ls` and a concrete id or slug.

## GLOBAL EXIT STATUS

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Generic error; also `rex status` when any session is `needs_input`; `rex doctor` when any check fails |
| 2 | Selector not found |
| 3 | Ambiguous selector |
| 4 | Daemon unreachable |
| 5 | Invalid arguments |
| 6 | Operation refused |
| 7 | `rex wait` timed out |

Unknown commands print to stderr and exit **1**.

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
rex ls [-socket path] [-state dir] [-tool id] [-model id] [-show-archived] [-short] [-json]
```

`-show-archived` and `-short` are accepted but have no effect today.

---

## rex new

Spawn a session.

```sh
rex new [prompt] [-socket path] [-tool id] [-model id] [-effort tier]
           [-slug name] [-cwd dir] [-fleet name] [-no-attach]
```

Defaults: `-tool echo`, `-model short`, `-cwd` `$PWD`. Prompt from arguments or stdin when stdin is not a TTY.

Prints `short_id` and `slug` tab-separated on success.

`-no-attach` is accepted but has no effect.

---

## rex attach

Attach terminal to a session PTY.

```sh
rex attach <sel> [-socket path] [-read-only] [-no-replay]
```

Detach with **Ctrl+]**. `-read-only` does not send keyboard input. `-no-replay` skips transcript tail.

---

## rex reply

Send text to a session, then press Enter (`\r`, sent as a separate write so full-screen TUIs submit instead of inserting a newline).

```sh
rex reply <sel> [text] [-socket path] [-raw]
```

Text from argument or stdin. `-raw` sends bytes without an added newline.

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
rex log <sel> [-socket path] [-state-dir dir] [-f] [-n lines] [-bytes N]
```

`-f` follows the file. Default state dir: `~/.local/share/rex`.

---

## rex wait

Block until a session reaches a state.

```sh
rex wait <sel> [-socket path] [-until state] [-timeout duration]
```

`-until`: `working`, `needs_input`, `done`, `failed`, or `any` (terminal states). Default `done`.

`-timeout` uses Go duration syntax (`30s`, `10m`). Exit **7** on timeout.

---

## rex rm

Delete a session.

```sh
rex rm <sel> [-socket path] [-force]
```

---

## rex rename

```sh
rex rename <sel> <new-slug> [-socket path]
```

---

## rex archive

Archive a completed session (title prefix).

```sh
rex archive <sel> [-socket path]
```

---

## rex complete

Mark a running session done.

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
          [-no-shell] [-config path] [-socket path] [-force]
```

---

## rex doctor

Diagnostics. Exit **1** if any check fails.

```sh
rex doctor [-json] [-socket path] [-verbose]
```

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

Dial or protocol errors exit **1** (not always `ExitCoder`).

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

`rex ls -short` and `rex new -no-attach` are no-ops. `rex --help` lists `@` selector aliases that the implementation does not support.
