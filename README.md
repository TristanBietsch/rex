# ∴ rex

rex runs your coding agents (Claude Code, Codex, Qwen Code, Cursor, Grok,
Antigravity, Ollama) side by side and shows them on one kanban board, so you
always know which one needs you.

It is for people who run more than one agent at a time in a terminal. Agents
keep running when you close the board. rex tells you when one asks a
question or finishes, and lets you jump in with one key.

```text
  ∴ REX    2 ready · 2 waiting · 3 working · 1 done  · 1 failed
  ──────────────────────────────────────────────────────────────────────────────
  READY  2
   ⚑  dark-mode                           ✦  ? asks           $0.31           4m
   ⚑  release-notes                                           $2.10          11m
  WAITING  2
   ◷  invoice-rounding-tests                 sh 42s           $0.84  ▴ 12m   40s
  WORKING  3
 ▸ ⠋  payment-migration-v2                             ⋔ 3    $4.92  ▴ 45m    1m
   ⠋  docs-sweep                                                              7s
  DONE  2
   ✓  test-coverage                       ✦                   $3.40           9m
  ──────────────────────────────────────────────────────────────────────────────
  λ press i to describe a task for a new session
  enter open · i new · cc done · tab next · ? more
```

The board at 80 columns, trimmed. Top to bottom: sessions that need you,
sessions busy on a tool, sessions working, finished sessions.

Runs on macOS, Linux, and Windows 10 1809+ / 11. Two binaries: `rex` (CLI
and board) and `rex-daemon` (keeps the agents alive).

## Install

macOS or Linux. Download the installer, read it, then run it:

```sh
curl -fsSL https://raw.githubusercontent.com/TristanBietsch/rex/master/install.sh -o install.sh
sh install.sh                      # or: REX_VERSION=v2.0.0 sh install.sh
export PATH="$HOME/.local/bin:$PATH"   # if the installer says it is not on PATH
rex setup
```

The installer downloads the latest
[release](https://github.com/TristanBietsch/rex/releases) for your OS and
arch, checks it against `SHA256SUMS`, and installs `rex` and `rex-daemon`
into `~/.local/bin` (`PREFIX=/some/dir` for another place). No sudo. It never
edits your shell profile. On Linux, sound needs ALSA (`libasound2`).

Windows (PowerShell):

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/TristanBietsch/rex/master/install.ps1 -OutFile install.ps1
powershell -ExecutionPolicy Bypass -File .\install.ps1   # or: -Version v2.0.0
rex setup
```

It installs `rex.exe` and `rex-daemon.exe` into `~\.local\bin` and prints
the command that adds it to PATH; it never edits PATH itself. Claude Code's
state hooks need Git for Windows.

From a checkout (needs the Go version in `go.mod`):

```sh
./install.sh --from-source         # Windows: .\install.ps1 -FromSource
```

With Go and no clone:

```sh
go install github.com/tristanbietsch/rex/cmd/rex@latest
go install github.com/tristanbietsch/rex/cmd/rex-daemon@latest
```

`@latest` builds the newest `master` commit. The module path has no `/v2`,
so Go ignores the `v2.x` tags.

Upgrade any install in place:

```sh
rex update        # -check only looks
rex upgrade       # load the new build; running agents keep going
```

## Start in 60 seconds

1. Open the board. It starts the daemon if needed.

   ```sh
   rex
   ```

2. Press `n`. Pick an agent, type a task, launch it.
3. Select the row and press `enter` to attach. You type straight into the
   agent.
4. Press `q` twice (`qq`) to detach. The agent keeps running.
5. When the work is finished, press `cc` to mark it done. Press `cc` again
   to undo.

Press `?` for every key and `S` for settings. Step-by-step with a test agent
that needs no API key: [docs/quickstart.md](docs/quickstart.md).

## How it works

- **One daemon, many terminals.** `rex-daemon` starts each agent in its own
  pseudo-terminal (PTY) and keeps it alive. The board and the CLI are
  clients; close them and nothing stops. See [docs/daemon.md](docs/daemon.md).
- **State from the agent, not guesses.** Claude Code reports each turn
  through hooks. Other agents are read from the screen. Each session sits in
  one of four sections:
  **READY** needs you (a question, or its turn ended),
  **WAITING** is busy on a shell command, subagent or web call,
  **WORKING** is thinking or typing, **DONE** finished or was marked done.
  See [docs/tui.md](docs/tui.md#description).
- **Hot upgrade.** `rex upgrade` re-execs the daemon in place: every agent
  keeps its process, PTY and screen, and open boards swap to the new build.
  Windows restarts cold. See [docs/daemon.md](docs/daemon.md#hot-restart).
- **Config in layers.** Lowest first: `config.yaml`, then
  `~/.config/rex/config.lua`, then a trusted workspace's `.rex/config.lua`.
  `rex reload` re-reads them all.
- **Trust is yours to give.** rex answers an agent's "do you trust this
  folder?" dialog only for folders you listed with `rex trust`. See
  [Security](#security).

## Concepts

| Term | Meaning |
|------|---------|
| [session](docs/glossary.md#session) | One agent process, its transcript and its metadata. |
| [board](docs/tui.md) | The full-screen kanban view you get from `rex`. |
| [READY](docs/tui.md#description) | `⚑`, in a bright green: the agent waits on you, a question or its turn ended. |
| [WAITING](docs/tui.md#description) | `◷`: the agent waits on a tool (shell, subagent, web), not on you. |
| [WORKING](docs/tui.md#description) | The agent is busy. |
| [DONE](docs/tui.md#mark-done) | `✓` in a calmer green (`✗` failed, `⊘` crashed): finished, failed, or marked done with `cc`. |
| [workspace](docs/workspaces.md) | A project folder with a `.rex/` folder: id, name, color, own board. |
| [loop](docs/loops.md) | Re-sends a prompt each turn until your check command passes. |
| [fleet](docs/cli.md#rex-fleet) | A label that groups related sessions. |
| [trust](docs/settings.md#auto-trust) | Folders where rex answers the agent's folder-trust dialog. |

## Common tasks

| Task | Command or key | Doc |
|------|----------------|-----|
| Open the board | `rex` | [tui.md](docs/tui.md) |
| Start an agent | `n` on the board, or `rex new -tool claude -- "fix the test"` | [cli.md](docs/cli.md#rex-new) |
| Attach / detach | `enter` / `qq` or `ctrl+]` | [tui.md](docs/tui.md#attach) |
| Jump to the next session that needs you | `tab` | [tui.md](docs/tui.md#keys-board-focus) |
| Mark done / undo | `cc` / `uc` | [tui.md](docs/tui.md#mark-done) |
| Resume an exited session | `uc`, or `rex resume <sel>` | [cli.md](docs/cli.md#rex-resume) |
| List sessions from a shell | `rex ls`, `rex status` | [cli.md](docs/cli.md#rex-ls) |
| Leave the board, keep agents | `:bg` | [tui.md](docs/tui.md#detach-without-quitting) |
| Change a setting | `S`, or `rex config set <id> <value>` | [settings.md](docs/settings.md) |
| Trust a project folder | `rex trust` | [cli.md](docs/cli.md#rex-trust) |
| Run a loop | `rex loop start <name>` (needs `loops`) | [loops.md](docs/loops.md) |
| Open a workspace board | `rex here` (needs `workspaces`) | [workspaces.md](docs/workspaces.md) |
| Undo an agent's turn | `rex undo <sel>` (needs `turn_snapshots`) | [snapshots.md](docs/snapshots.md) |
| Export a transcript | `rex export <sel>` | [export.md](docs/export.md) |
| Check the install | `rex doctor` | [cli.md](docs/cli.md#rex-doctor) |
| Script rex from CI or an agent | `rex --agent ...`, `rex wait` | [howto/headless-ci.md](docs/howto/headless-ci.md) |

`loops`, `workspaces` and `turn_snapshots` are off by default. Turn one on
with `rex config set <id> true`.

## Configuration

| What | Where | Doc |
|------|-------|-----|
| Every file and folder rex uses | `~/.config/rex/`, `~/.local/share/rex/`, `~/.local/state/rex/` | [paths.md](docs/paths.md) |
| Settings | `~/.config/rex/config.yaml`, the `S` page, `rex config` | [settings.md](docs/settings.md) |
| Settings in Lua | `~/.config/rex/config.lua` | [howto/lua-config.md](docs/howto/lua-config.md) |
| Per-project display settings | `<workspace>/.rex/config.lua` | [workspaces.md](docs/workspaces.md#workspace-settings) |
| Themes and accent color | `theme`, `accent_color`, `~/.config/rex/themes/` | [themes.md](docs/themes.md) |
| Your own agents | `~/.config/rex/tools.yaml` | [registry.md](docs/registry.md) |
| Lua hooks | `~/.config/rex/init.lua` | [howto/lua-hooks.md](docs/howto/lua-hooks.md) |

Set the accent on any theme:

```sh
rex config set accent_color very-peri    # or "#ff6f61", "oklch(0.7 0.15 30)"
```

## Security

- **Trust is opt-in.** No folder is trusted by default. `rex trust` adds the
  repo folder you are in; it never trusts a parent, your home folder or `/`.
- **`skip_permissions` is off by default** and applies only in folders
  under `auto_trust_dirs`.
- **Trust changes need a person at a terminal.** `rex trust`, `rex ws trust`
  and setting `skip_permissions` or `auto_trust_dirs` exit 6 in agent mode,
  with `-o`, or in a pipe. An agent cannot widen its own permissions.
- **Workspace Lua cannot grant trust.** A repo's `.rex/config.lua` runs only
  after `rex ws trust`, only for display settings, and stops running when
  its bytes change.
- **Releases are checked.** The installers and `rex update` verify
  `SHA256SUMS` and refuse a mismatch. Each archive carries a GitHub build
  provenance attestation:

  ```sh
  gh attestation verify rex_2.0.0_darwin_arm64.tar.gz --repo TristanBietsch/rex
  ```

rex has no network listener; clients talk to the daemon over a local Unix
socket. Threat model and how to report a bug: [SECURITY.md](SECURITY.md).

## Upgrading from v1

The first switch to v2 restarts the daemon cold once: the v1 daemon cannot
hand its agents over, so running agents end. Let them finish first, then run
`rex upgrade` from a terminal outside rex. Later upgrades keep every session.

Steps and breaking changes: [docs/releases/v2.0.0.md](docs/releases/v2.0.0.md).
Every change: [CHANGELOG.md](CHANGELOG.md).

## Docs

| Doc | What it covers |
|-----|----------------|
| [docs/index.md](docs/index.md) | Documentation map by audience |
| [docs/quickstart.md](docs/quickstart.md) | Install through the first session, step by step |
| [docs/tui.md](docs/tui.md) | The board: sections, keys, attach, pages |
| [docs/slash.md](docs/slash.md) | `:` commands on the board |
| [docs/cli.md](docs/cli.md) | Every `rex` command, flag and exit code |
| [docs/settings.md](docs/settings.md) | Every setting, its default and when it applies |
| [docs/themes.md](docs/themes.md) | Built-in and bundled themes, accent color, theme files |
| [docs/paths.md](docs/paths.md) | Files, folders, socket and environment variables |
| [docs/workspaces.md](docs/workspaces.md) | Workspaces, workspace settings, audit trail |
| [docs/loops.md](docs/loops.md) | Loop workflows |
| [docs/snapshots.md](docs/snapshots.md) | Turn snapshots and undo |
| [docs/export.md](docs/export.md) | Export a session as a document |
| [docs/registry.md](docs/registry.md) | The agent catalog and `tools.yaml` |
| [docs/daemon.md](docs/daemon.md) | `rex-daemon`: options, signals, hot restart |
| [docs/protocol.md](docs/protocol.md) | The JSON wire protocol on the socket |
| [docs/architecture.md](docs/architecture.md) | Processes and data flow |
| [docs/STRUCTURE.md](docs/STRUCTURE.md) | Repository layout |
| [docs/glossary.md](docs/glossary.md) | Project terms |
| [docs/UNDOCUMENTED.md](docs/UNDOCUMENTED.md) | Known unclear behavior |
| [docs/howto/agents.md](docs/howto/agents.md) | Drive rex from an LLM agent |
| [docs/howto/headless-ci.md](docs/howto/headless-ci.md) | Run sessions without the board |
| [docs/howto/add-a-tool.md](docs/howto/add-a-tool.md) | Add an agent CLI to the registry |
| [docs/howto/customize-appearance.md](docs/howto/customize-appearance.md) | Colors, density, sounds, header style |
| [docs/howto/lua-config.md](docs/howto/lua-config.md) | Settings in `config.lua` |
| [docs/howto/lua-hooks.md](docs/howto/lua-hooks.md) | Daemon hooks in `init.lua` |
| [docs/howto/shell-integration.md](docs/howto/shell-integration.md) | Shell prompt and completion |
| [docs/howto/skills.md](docs/howto/skills.md) | Load skills into one session |
| [docs/releases/v2.0.0.md](docs/releases/v2.0.0.md) | v2.0.0 release notes and migration |
| [CHANGELOG.md](CHANGELOG.md) | Every change, by release |
| [SECURITY.md](SECURITY.md) | Threat model and reporting |
| [CONTRIBUTING.md](CONTRIBUTING.md) | How to contribute |
| [docs/agents/](docs/agents/README.md) | Guide for contributors and coding agents |

Go source wins when a doc disagrees with behavior.

## Contributing

Start with [CONTRIBUTING.md](CONTRIBUTING.md). Coding agents start with
[docs/agents/HANDOFF.md](docs/agents/HANDOFF.md). Build, test and release
commands are in [docs/agents/WORKFLOWS.md](docs/agents/WORKFLOWS.md).

```sh
make build        # ./rex and ./rex-daemon
make check        # vet, race tests, lint, coverage, vuln scan
```

## License

MIT. See [LICENSE](LICENSE).
