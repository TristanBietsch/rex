# CLI

The CLI is a peer of the TUI, not an afterthought. Every TUI action has a non-interactive equivalent, output is line-oriented and pipe-friendly, errors go to stderr, and exit codes are meaningful. The whole surface should fit muscle memory: `rex <verb> <selector>`.

## Conventions

### Selectors

Anywhere `<selector>` appears, accept all three forms:

- **short id** — the 4-char hex from the board (e.g. `7d4f`)
- **slug** — kebab-case session name (e.g. `payment-migration`)
- **full UUID** — for scripts and pipelines

If a selector matches more than one session, the command exits `3` and prints the candidates to stderr.

### Output modes

- **Text** (default) — columnar, human-readable, color when `stdout` is a TTY, no color otherwise.
- **JSON** (`--json`) — one JSON object per line (JSONL). Same shape as the protocol's `SessionSummary` when listing sessions.

### Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success |
| 1 | Generic error |
| 2 | Selector not found |
| 3 | Selector ambiguous (multiple matches) |
| 4 | Daemon unreachable and auto-start failed |
| 5 | Invalid arguments |
| 6 | Operation refused by state (e.g. `archive` on a non-completed session) |
| 7 | Wait timed out (`rex wait`) |

### Streams

- `stdout` carries data — table rows, JSON, transcript bytes.
- `stderr` carries diagnostics — error messages, warnings, daemon log spillover.
- `stdin` is used by `rex send` (and `rex new` when piping a prompt).

---

## `rex`

No arguments — open the TUI. If `~/.local/state/rex/tui-state.json` exists (from a prior `:bg`), the saved selection / filter / scroll are restored and the state file is removed.

```
rex [flags]

Flags:
  --socket PATH        override UDS path (default: $XDG_RUNTIME_DIR/rex.sock)
  --no-animation       disable all transitions and spinners
  --no-audio           disable audio (overrides config)
  --ascii              force ASCII fallback glyphs
  --show-archived      include archived sessions in the Completed section
  --fresh              ignore tui-state.json and start on a clean board
```

---

## `rex status`

One-liner aggregate. Designed for shell prompts and quick "anything waiting?" checks.

```
$ rex status
2 awaiting input · 4 working · 1 completed

$ rex status --json
{"awaiting_input":2,"working":4,"completed":1,"failed":0,"crashed":0}
```

Exit code is `1` when one or more sessions are `needs_input`, `0` otherwise. Useful in shell prompt customizations as an ambient indicator.

---

## `rex ls`

List sessions. Table by default, JSONL with `--json`.

```
rex ls [flags]

Flags:
  --state STATE        filter by state: working | needs_input | done | failed | crashed
  --tool TOOL          filter by tool id (claude, codex, gemini, ollama, …)
  --model MODEL        filter by model id
  --show-archived      include archived sessions
  --short              one-line-per-session compact mode
  --json               JSONL output (one SessionSummary per line)
```

Default text columns: `ID  STATE  TOOL  MODEL  SLUG  LAST EVENT`.

Examples:

```sh
# what needs my attention?
rex ls --state needs_input

# what's running on Claude?
rex ls --tool claude --state working

# pipe to jq
rex ls --json | jq 'select(.state=="needs_input") | .slug'
```

---

## `rex new`

Spawn a session. With no flags, opens the wizard in the TUI. With flags, bypasses it.

```
rex new [PROMPT...] [flags]

Flags:
  --tool TOOL          tool id (default: configured default)
  --model MODEL        model id within the tool
  --effort EFFORT      reasoning effort (when applicable)
  --slug SLUG          session slug (default: derived from prompt or timestamp)
  --cwd PATH           working directory (default: $PWD)
  --no-attach          don't open the modal after spawning (default: attach in TUI mode, exit in CLI mode)
```

Examples:

```sh
# spawn with positional prompt
rex new --tool claude --model sonnet "fix flaky billing test"

# read the prompt from stdin (heredoc, file, pipe)
rex new --tool codex --model gpt-5-codex --slug billing < prompt.md

# script-friendly: spawn and don't open anything
rex new --tool ollama --model llama3.1 --no-attach "summarize TODO.md"
```

---

## `rex attach <selector>`

Connect the current terminal to a session's PTY — the same view as the TUI modal, but full-screen and without the board chrome behind it. Bytes flow both directions until you detach.

```
rex attach <selector> [flags]

Flags:
  --read-only          attach as observer; keystrokes don't forward
```

While attached:

- Keystrokes forward to the session's PTY as raw bytes.
- Output streams to your terminal in real time.
- **Detach key:** `ctrl+a d` (a familiar `screen`/`tmux` convention; doesn't collide with any agent's own keybinds).
- Resize is honored — the agent receives `SIGWINCH` when your terminal resizes.

This is the CLI equivalent of double-clicking a row in the TUI: it gets you straight to the agent process, no board, no menu. Exit when you're done.

Examples:

```sh
rex attach 7d4f                    # by short id
rex attach payment-migration       # by slug
rex attach 7d4f-3c8a-…             # by full UUID
rex attach 7d4f --read-only        # observe without interfering
```

---

## `rex reply <selector> [TEXT...]`

Send a one-shot reply to a session that's in `needs_input`. The text gets a trailing newline appended (use `--raw` to skip). Without positional args, reads from stdin.

```
rex reply 7d4f "system default — no toggle"
rex reply b203 "feature A leads, then B"

echo "yes" | rex reply 9e51
cat draft.md | rex reply 9e51 --raw
```

Exit `6` if the session isn't in `needs_input`.

---

## `rex send <selector>`

Lower-level: forward raw stdin to the session's PTY. No newline mangling. Useful for piping anything (logs, key sequences, ANSI escapes) directly into the agent.

```
printf 'y\n' | rex send 7d4f
ssh-add -L | rex send 9e51
```

---

## `rex log <selector>`

Print the session's transcript.

```
rex log <selector> [flags]

Flags:
  -n N           show only the last N lines (default: all)
  -f, --follow   stream new output as it arrives (like `tail -f`)
  --bytes        print raw PTY bytes (including ANSI); default strips control chars
```

Examples:

```sh
rex log 7d4f                # full transcript
rex log -n 50 7d4f          # last 50 lines
rex log -f 9e51             # live-tail
rex log --bytes 7d4f | less # raw, with colors preserved if your pager honors them
```

---

## `rex wait <selector>`

Block until the selector's state reaches a target. Useful for scripts that orchestrate agents.

```
rex wait <selector> [flags]

Flags:
  --until STATE          working | needs_input | done | failed | (default: any terminal state)
  --timeout DURATION     e.g. 30s, 5m, 1h; exits 7 on timeout (default: no timeout)
```

Examples:

```sh
# block this shell until the migration finishes
rex wait payment-migration --until done

# script that waits up to 10 minutes, fails if it didn't finish
rex wait payment-migration --until done --timeout 10m || echo "still running"
```

---

## `rex rename <selector> <new-slug>`

Change a session's slug. The short id is unaffected.

```
rex rename 7d4f dark-mode-v2
```

---

## `rex archive <selector>`

Move a `done` session out of the visible Completed list. Exit `6` if the session isn't `done`. Use `rex ls --show-archived` to see archived sessions.

```
rex archive 2b7e
```

---

## `rex complete <selector>`

Cleanly terminate a session and mark it `done`. The daemon kills the PTY and transitions the session to the terminal `done` state. Exit `0` on success, `2` if the selector isn't found, `3` if ambiguous. No-op if the session is already terminal.

Use this for tools whose prompt is identical between "awaiting" and "just finished" (codex, gemini, ollama) — when the heuristic adapter can't tell completion apart from `needs_input`, `rex complete` is the explicit signal.

```
rex complete 7d4f
rex complete payment-migration
```

Distinct from `rex rm` (which deletes the session entirely) and `rex archive` (which only hides an already-`done` session).

---

## `rex rm <selector>`

Delete a session. Terminates its PTY (if alive), removes its session directory (or moves to `trash/` when `soft_trash` is enabled in `config.yaml`).

```
rex rm 7d4f
rex rm 7d4f --force        # skip confirmation when run with stdin attached to a TTY
```

When `stdin` is a TTY and `--force` is not set, `rex rm` prompts `delete 7d4f (dark-mode)? [y/N]` before proceeding. When `stdin` is *not* a TTY (script context), it proceeds without prompting.

---

## `rex reload`

Re-read `~/.config/rex/tools.yaml`, `~/.config/rex/config.yaml`, and `~/.config/rex/init.lua`. Equivalent to `kill -HUP $(pgrep rex-daemon)`. Use after editing the registry or settings.

```
rex reload
```

---

## `rex slash`

Run a slash command from the shell (see `slash.md`). Mirrors the `/` palette in the TUI.

```
rex slash list [--json]            # list every registered slash command
rex slash <id> [args...]           # run a slash command non-interactively
```

Custom Lua-registered slash commands work over the CLI too — they execute in a one-shot Lua runtime that connects to the daemon, runs the action, and exits.

Examples:

```sh
rex slash list
rex slash find dark-mode
rex slash attach 7d4f
rex slash standup                  # user-defined via init.lua
```

---

## `rex config`

Manage settings (see `settings.md` for the full registry). Mirrors the TUI settings page for non-interactive use.

```
rex config list [--json]              # print every setting and its current value
rex config get <id>                   # print one value
rex config set <id> <value>           # set one value (writes config.yaml)
rex config reset <id>                 # reset to default
rex config edit                       # open init.lua in $EDITOR
```

`rex config set` validates against the setting's type/range/options; exits non-zero with a useful diagnostic on rejection. Setting that has only one option in v0 (e.g. `color_scheme=default`) accepts that option and rejects others.

Examples:

```sh
rex config get spinner
rex config set max_concurrent_sessions 24
rex config set master_volume 0.5
rex config reset prompt_glyph
rex config list --json | jq 'select(.section=="Audio")'
```

---

## `rex daemon`

Subcommands for managing the supervisor process directly. Most users never need these — `rex` auto-starts the daemon on demand.

```
rex daemon start            # start daemon if not running
rex daemon stop             # graceful shutdown (terminates all sessions)
rex daemon restart          # stop + start
rex daemon status           # is the daemon up? pid / socket / uptime
rex daemon logs [-f]        # tail ~/.local/state/rex/daemon.log
```

---

## `rex-daemon`

The supervisor binary. Normally invoked via `rex daemon start` or auto-started by `rex`. Direct invocation is exposed for systemd, launchd, or debugging.

```
rex-daemon [flags]

Flags:
  --socket PATH                   override UDS path
  --state-dir PATH                override state dir (default: ~/.local/share/rex)
  --max-concurrent-sessions N     default 16
  --log-level LEVEL               debug | info | warn | error (default: info)
  --foreground                    do not daemonize; print logs to stderr
```

---

## Composability

The CLI is designed to be piped, scripted, and aliased. Some recipes:

```sh
# slug → short id lookup
rex ls --json | jq -r 'select(.slug=="payment-migration") | .short_id'

# count sessions per tool
rex ls --json | jq -r .tool_id | sort | uniq -c

# alias an agent's transcript to my $EDITOR
alias rex-edit='rex log "$1" | $EDITOR -'

# notify me when any session needs input (poll-based)
while sleep 30; do rex status >/dev/null || say "agent needs you"; done

# block a CI step on an agent's completion
rex new --tool claude --model opus --slug ci-fixes "$prompt" --no-attach
rex wait ci-fixes --until done --timeout 30m
test "$(rex ls --json | jq -r 'select(.slug=="ci-fixes") | .state')" = done
```

Tab completion: shipped for bash, zsh, and fish via `rex completion <shell>`. The output is the cobra-style completion script; source it from your rc file.
