# rex usage

## Synopsis

```
rex [command] [flags]
```

With no arguments, `rex` opens the TUI. If `~/.local/state/rex/tui-state.json`
exists (from a prior `:bg`), the saved selection, filter, and scroll are
restored and the state file is removed.

---

## Commands

Commands are grouped by workflow: session management first, then operations and
lifecycle, then utility.

### `rex status`

One-line aggregate. Exits `1` when any session is `needs_input`, `0` otherwise.

```sh
rex status                  # 2 awaiting input · 4 working · 1 completed
rex status --json           # {"awaiting_input":2,"working":4,"completed":1,...}
```

Useful in shell prompts as an ambient indicator.

---

### `rex ls`

List sessions. Table by default, JSONL with `--json`.

```sh
rex ls [--state STATE] [--tool TOOL] [--model MODEL] [--show-archived] [--short] [--json]
```

```sh
rex ls --state needs_input
rex ls --tool claude --state working
rex ls --json | jq 'select(.state=="needs_input") | .slug'
```

Default columns: `ID  STATE  TOOL  MODEL  SLUG  LAST EVENT`.

---

### `rex new`

Spawn a session. With no flags, opens the wizard in the TUI. With flags,
bypasses it.

```sh
rex new [PROMPT...] [--tool TOOL] [--model MODEL] [--effort EFFORT] \
        [--slug SLUG] [--cwd PATH] [--fleet FLEET] [--no-attach]
```

```sh
rex new "fix flaky billing test" --tool claude --model sonnet
rex new --tool codex --model gpt-5-codex --slug billing < prompt.md
rex new "load test" --fleet=launch --no-attach
```

`--fleet` assigns the session to a named fleet; sessions in a fleet get a
tinted border on the board.

---

### `rex attach`

Connect the current terminal to a session's PTY — full-screen, no board chrome.
Bytes flow both directions until you detach.

```sh
rex attach <selector> [--read-only]
```

```sh
rex attach 7d4f
rex attach payment-migration
rex attach 7d4f --read-only        # observe without interfering
```

Detach key: `ctrl+a d`. Resize is honored — the agent receives `SIGWINCH` when
your terminal resizes.

---

### `rex reply`

Send a one-shot reply to a session that is in `needs_input`. Appends a trailing
newline unless `--raw` is given. Reads from stdin when no positional args are
provided.

```sh
rex reply <selector> [TEXT...] [--raw]
```

```sh
rex reply 7d4f "ship it"
echo "yes" | rex reply 9e51
cat draft.md | rex reply 9e51 --raw
```

Exits `6` if the session is not in `needs_input`.

---

### `rex send`

Forward raw stdin to a session's PTY. No newline mangling. Lower-level than
`reply` — useful for piping key sequences, ANSI escapes, or arbitrary bytes.

```sh
rex send <selector>
```

```sh
printf 'y\n' | rex send 7d4f
ssh-add -L | rex send 9e51
```

---

### `rex log`

Print or tail a session's transcript.

```sh
rex log <selector> [-n N] [-f | --follow] [--bytes]
```

```sh
rex log 7d4f                # full transcript
rex log -n 50 7d4f          # last 50 lines
rex log -f 9e51             # live-tail
rex log --bytes 7d4f | less # raw bytes, ANSI preserved
```

`--bytes` emits raw PTY bytes including control sequences; default strips them.

---

### `rex wait`

Block until the selector's state reaches a target. Exits `7` on timeout.

```sh
rex wait <selector> [--until STATE] [--timeout DURATION]
```

```sh
rex wait payment-migration --until done
rex wait payment-migration --until done --timeout 10m || echo "still running"
```

`--until` accepts: `working`, `needs_input`, `done`, `failed`. Default: any
terminal state.

---

### `rex rename`

Change a session's slug. Short id is unaffected.

```sh
rex rename <selector> <new-slug>
```

```sh
rex rename 7d4f dark-mode-v2
```

---

### `rex rm`

Delete a session. Terminates its PTY if alive; removes the session directory
(or moves to `trash/` when `soft_trash` is enabled).

```sh
rex rm <selector> [--force]
```

When stdin is a TTY and `--force` is not set, prompts for confirmation.

---

### `rex archive`

Move a `done` session out of the visible Completed list. Exits `6` if not
`done`.

```sh
rex archive <selector>
```

Use `rex ls --show-archived` to see archived sessions.

---

### `rex digest`

Print a daily summary: sessions started, time spent, token totals. Scoped to
today by default.

```sh
rex digest
```

---

### `rex stats`

Print lifetime usage statistics across models and tools. Only models with
non-zero usage are shown.

```sh
rex stats
rex stats --json
```

---

### `rex fleet`

Manage fleets — named groups of related sessions. Sessions in a fleet get a
tinted border on the board.

```sh
rex fleet ls                        # list all fleets
rex fleet set <session> <fleet>     # assign a session to a fleet
rex fleet unset <session>           # remove fleet assignment
rex fleet show <fleet>              # list sessions in a fleet
```

```sh
rex fleet ls
rex fleet set load-test launch
rex fleet show launch
```

---

### `rex reload`

Re-read `tools.yaml`, `config.yaml`, and `init.lua`. Equivalent to
`kill -HUP $(pgrep rex-daemon)`.

```sh
rex reload
```

---

### `rex setup`

Guided first-run wizard. Walks through config path, daemon setup, default
provider, privacy defaults, and shell integration. Non-destructive — previews
changes before writing to `~/.config/rex`.

```sh
rex setup
```

Ends with a working command so the first success happens in under a minute.

---

### `rex doctor`

Diagnostic check. Verifies PATH, daemon reachability, config dir writability,
and provider key presence (values never printed). CI-friendly exit codes.

```sh
rex doctor
```

---

### `rex update`

Upgrade rex in place. Downloads the latest release, verifies checksums, and
replaces the installed binaries.

```sh
rex update
```

---

### `rex uninstall`

Remove the rex binaries. Optionally strips the shell-init block and wipes all
state and session data.

```sh
rex uninstall [--wipe-state]
```

`--wipe-state` removes `~/.local/share/rex` and `~/.local/state/rex` after
explicit confirmation.

---

### `rex daemon`

Subcommands for managing the supervisor directly. Most users never need these.

```sh
rex daemon start
rex daemon stop
rex daemon restart
rex daemon status           # pid, socket path, uptime
rex daemon logs [-f]        # tail ~/.local/state/rex/daemon.log
```

---

### `rex config`

Manage settings. Mirrors the TUI settings page for non-interactive use.

```sh
rex config list [--json]
rex config get <id>
rex config set <id> <value>
rex config reset <id>
rex config edit                     # open config.yaml in $EDITOR
```

```sh
rex config get spinner
rex config set max_concurrent_sessions 24
rex config set master_volume 0.5
rex config list --json | jq 'select(.section=="Audio")'
```

`rex config set` validates against the setting's type, range, and allowed
values.

---

### `rex render`

Render an event stream (for debugging and pipe recipes). Reads JSONL from
stdin and emits formatted output.

```sh
rex render
```

---

### `rex completion`

Generate shell completion scripts.

```sh
rex completion bash
rex completion zsh
rex completion fish
```

Source the output from your shell rc file, or pipe it directly:

```sh
rex completion zsh > ~/.zsh/completions/_rex
```

---

### `rex version`

Print the version string and exit.

```sh
rex version
```

---

## Selectors

Anywhere `<selector>` appears, three forms are accepted:

| Form | Example | Notes |
| --- | --- | --- |
| Short id | `7d4f` | First four hex chars of the session UUID |
| Slug | `payment-migration` | Kebab-case name set at creation or via `rename` |
| Full UUID | `7d4f3c8a-…` | For scripts and pipelines |
| `@needs` | `@needs` | All sessions in `needs_input` state |
| `@working` | `@working` | All sessions in `working` state |
| `@done` | `@done` | All sessions in `done` state |

If a selector matches more than one session, the command exits `3` and prints
the candidates to stderr.

---

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success |
| 1 | Generic error / something needs input (`rex status`) |
| 2 | Selector not found |
| 3 | Selector ambiguous — multiple matches |
| 4 | Daemon unreachable and auto-start failed |
| 5 | Invalid arguments |
| 6 | Operation refused by state (e.g. `archive` on a non-done session) |
| 7 | Wait timed out (`rex wait --timeout`) |

---

## Files

| Path | Purpose |
| --- | --- |
| `~/.config/rex/config.yaml` | User settings (written by `rex config set`) |
| `~/.config/rex/tools.yaml` | Tool and model registry overrides |
| `~/.config/rex/init.lua` | Optional Lua hooks, loaded at daemon start and `rex reload` |
| `~/.local/state/rex/daemon.log` | Daemon structured log |
| `~/.local/state/rex/tui-state.json` | Saved TUI state written by `:bg`, consumed at next launch |
| `~/.local/share/rex/sessions/<id>/meta.json` | Session metadata |
| `~/.local/share/rex/sessions/<id>/transcript.log` | Rolling PTY transcript |
| `$XDG_RUNTIME_DIR/rex.sock` | IPC — Unix domain socket, JSONL protocol |

---

## Environment

| Variable | Effect |
| --- | --- |
| `REX_LOG_DIR` | Override the directory where rex writes log files (default: `~/.local/state/rex/`) |
| `REX_LOG_LEVEL` | Log verbosity: `debug`, `info`, `warn`, `error` (default: `info`) |

---

## See also

- [`docs/cli.md`](cli.md) — full flag reference for every command
- [`docs/architecture.md`](architecture.md) — two-process model, IPC, persistence
- [`docs/settings.md`](settings.md) — settings registry and Lua layer
- [`docs/registry.md`](registry.md) — adding tools and models
