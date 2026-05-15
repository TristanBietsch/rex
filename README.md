# rex

**Runtime Executive for Agents** — a terminal kanban for AI coding-agent
sessions. Spawn many agents in parallel across Claude Code, Codex, Gemini,
Ollama, and anything else with a CLI; rex shows what each one is doing,
which need your input, and lets you jump into any of them like opening a tab.

```
  ∴ REX
  2 awaiting input · 4 working · 1 completed                          [+ new]
  all · claude · codex · gemini · ollama
  ─────────────────────────────────────────────────────────────────────────
  Needs input
  ◆ 7d4f  dark-mode          system theme vs explicit toggle?     opus · high   4m
  ◆ b203  release-notes      draft ready — which feature leads?   gpt-5 · med  11m

  Working
  ⠋ 3c8a  perf-audit         events_org_ts index live — p95 38ms  sonnet         7m
  ⠋ 9e51  payment-migration  porting billing to the new processor gpt-5-codex    2m
  ⠋ c14a  load-test          k6 against the launch traffic …      llama3.1       3m

  Completed
  ●  2b7e  test-coverage     billing/ from 61% → 92% — PR #408    haiku          9m
```

Two binaries: `rex` is the Bubble Tea TUI, `rex-daemon` is the PTY supervisor.
They speak newline-delimited JSON over a Unix domain socket. The daemon
auto-starts the first time you run `rex` — you should never need to start
it by hand.

Pure Go, no cgo.

## Install

Pick whichever is easiest.

**One-liner from a checkout** — builds and drops binaries in `~/.local/bin`:

```sh
./install.sh
```

`install.sh --verbose` streams the build output; otherwise it spins quietly.

**Makefile** — same thing, configurable prefix:

```sh
make install                       # ~/.local/bin
make install PREFIX=/usr/local     # /usr/local/bin (may need sudo)
```

**`go install`** — if you don't want to clone:

```sh
go install github.com/tristanbietsch/rex/cmd/rex@latest
go install github.com/tristanbietsch/rex/cmd/rex-daemon@latest
```

All three install both binaries. Make sure the install directory is on your
`PATH`:

```sh
export PATH="$HOME/.local/bin:$PATH"   # if you used the defaults
```

To remove:

```sh
make uninstall   # or: rm ~/.local/bin/rex ~/.local/bin/rex-daemon
```

## Quickstart

```sh
rex                                 # launch the TUI
rex status                          # "2 awaiting · 4 working · 1 completed"
rex ls                              # session table (--json for JSONL)
rex new "fix flaky billing test" --tool claude --model sonnet
rex attach 7d4f                     # full-screen PTY (ctrl+] detaches)
rex reply 7d4f "ship it"            # answer a needs-input session
rex log -f 9e51                     # tail a session's transcript
rex wait payment-migration --until done --timeout 10m
```

Selectors accept the **short id** (`7d4f` — first four chars of the UUID),
the **slug** (`dark-mode`), or the full UUID. The CLI is a peer of the TUI,
not an afterthought: every TUI action has a non-interactive equivalent,
output is pipe-friendly, and exit codes are meaningful (`2` = not found,
`3` = ambiguous, `7` = `rex wait` timed out, etc.).

## TUI

Three sections, one focus, vim keybinds, mouse where you'd expect it.

| Key | Action |
| --- | --- |
| `j` `k` `↓` `↑` | Move row selection |
| `1` `2` `3` | Jump to Needs input / Working / Completed |
| `enter` | Open the modal on the selected session |
| `space` | Peek the last turn (or inline reply if `needs_input`) |
| `n` | New-agent wizard |
| `t` | Cycle tool filter chip |
| `r` / `a` / `dd` | Rename / archive / delete |
| `i` | Focus the bottom `λ` prompt |
| `:` | Command mode (`:new`, `:attach <sel>`, `:reply <sel> …`, `:bg`, `:q`, …) |
| `S` | Settings page |
| `?` | Help overlay |

`:bg` detaches the TUI but leaves the daemon and every session running.
The next `rex` invocation restores your selection, filter, and scroll.

## CLI

| Command | Purpose |
| --- | --- |
| `rex` | Launch the TUI |
| `rex status` | One-line aggregate (exit `1` when something needs input) |
| `rex ls [--state …] [--tool …] [--json]` | List sessions |
| `rex new [prompt] [--tool] [--model] [--cwd]` | Spawn a session |
| `rex attach <sel> [--read-only]` | Full-screen PTY attach |
| `rex reply <sel> [text]` | Reply to a needs-input session |
| `rex send <sel>` | Pipe raw stdin to a session's PTY |
| `rex log <sel> [-n N] [-f] [--bytes]` | Print or tail the transcript |
| `rex wait <sel> [--until] [--timeout]` | Block until state changes |
| `rex rename <sel> <slug>` | Rename a session |
| `rex archive <sel>` | Hide a completed session |
| `rex rm <sel>` | Delete a session |
| `rex reload` | Re-read `tools.yaml`, `config.yaml`, `init.lua` (SIGHUP) |
| `rex config {get,set,list,reset,edit}` | Manage settings |
| `rex daemon {start,stop,restart,status,logs}` | Manage the supervisor directly |
| `rex completion {bash,zsh,fish}` | Generate shell completions |

`rex --help` prints the full menu.

## Where things live

```
~/.local/share/rex/sessions/<id>/   meta.json + transcript.log (per session)
~/.local/state/rex/                 daemon.log, tui-state.json (saved by :bg)
~/.config/rex/                      tools.yaml, config.yaml, init.lua
$XDG_RUNTIME_DIR/rex.sock           IPC (Unix domain socket, JSONL)
```

Adding a new model is a one-entry edit to `~/.config/rex/tools.yaml`, which
merges over the built-in registry — no rebuild required. See
[`docs/registry.md`](docs/registry.md).

## How it works

```
┌────────────────────┐   UDS, JSONL    ┌──────────────────────────────┐
│  rex (TUI client)  │ ◀────────────▶ │  rex-daemon (PTY supervisor) │
│  stateless render  │                  │  state store + adapters      │
└────────────────────┘                  └──────────────┬───────────────┘
                                                        │ creack/pty
                                                        ▼
                                          claude · codex · gemini · ollama · …
```

The daemon owns the child processes and persists each session's metadata and
rolling transcript on disk; the TUI is a stateless renderer over a snapshot +
diff stream. Quit the TUI with `q` and your sessions keep running — reattach
any time with `rex`. The socket and JSONL protocol are deliberately
debuggable with `socat`, `jq`, and `tail`.

## Docs

The `docs/` directory is the source of truth for design and protocol:

- [`spec.md`](docs/spec.md) — v0 scope and non-goals
- [`architecture.md`](docs/architecture.md) — two-process model, IPC, persistence
- [`protocol.md`](docs/protocol.md) — JSONL message vocabulary
- [`cli.md`](docs/cli.md) — every command, flag, and exit code
- [`tui.md`](docs/tui.md) — screen anatomy, keybinds, state machine
- [`registry.md`](docs/registry.md) — adding tools and models
- [`settings.md`](docs/settings.md) — the settings registry and Lua layer
- [`slash.md`](docs/slash.md) — the `/` command palette
- [`design.md`](docs/design.md) — palette, typography, audio, animation
- [`stack.md`](docs/stack.md) — libraries and engineering constraints
