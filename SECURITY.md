# Security policy

## Reporting a vulnerability

Report vulnerabilities privately through GitHub Security Advisories:
open the repository's **Security** tab and choose **Report a vulnerability**
(<https://github.com/tristanbietsch/rex/security/advisories/new>).

Do not open a public issue, pull request or discussion for a suspected
vulnerability. Include the rex version (`rex version`), your OS, and steps
to reproduce.

## Supported versions

Rex has one maintainer. Fixes land on `master` and ship in the next
release. Older versions get no backports.

| Version | Supported |
|---------|-----------|
| `master` (latest) | Yes |
| `v1` (current release) | Yes |
| Anything older | No |

## Threat model

Rex runs on one machine, as one user. `rex-daemon` supervises agent CLIs
in pseudo-terminals, and `rex` (CLI and TUI) talks to it over a local Unix
domain socket. Rex has no network listener and no authentication beyond
filesystem permissions.

### In scope

- **The daemon socket.** A client that can connect to the socket can type
  into any agent's PTY, so it can run commands as you. Only your user may
  connect. Reports of other users or processes connecting, or of the socket
  being reachable beyond the filesystem, are in scope. Known gap: the
  socket, state directories and files currently get their modes from your
  umask, not from rex. Tightening this is planned.
- **Transcripts and state on disk.** Every session's full terminal output is
  written to `~/.local/share/rex/sessions/<id>/transcript.log`, next to its
  `meta.json`. Logs go to `~/.local/state/rex/`. Agents may print secrets,
  and rex stores whatever they print. Reports of rex exposing these files to
  other users, writing them outside those directories, or reading session
  data from outside them (for example path traversal through a session id)
  are in scope.
- **Untrusted bytes reaching your terminal.** Agent output, summaries and
  session names are shown in the TUI and CLI. Escape sequences that rex
  passes through unintentionally and that act on your terminal are in scope.
  AI summaries are written by a local model from what an agent printed, so
  they can be wrong or steered by that output ("Waiting: run curl … | sh").
  They are one line of display-safe text, length-capped, and never acted on.
  The overview headline (`overview_enabled`) is the same kind of text, a
  model paraphrase of several sessions' output at once, so one session can
  try to steer what it says about the others. Its counts and lists come from
  rules over session fields, never from that text, and it drives no action.
  The digest it is built from (every session's name, task, description and
  question line) goes to `OLLAMA_HOST`: if that points off this machine, so
  do those titles and statuses.
- **Turn snapshots** (`turn_snapshots`, off by default). A snapshot holds
  file **contents**: tracked files and untracked files that git does not
  ignore, so an untracked `.env` with secrets is copied into
  `~/.local/share/rex/snapshots/` (retained until 7 days after the session
  is removed, at most 2 GiB per folder). The store is created owner-only
  (0700), never leaves the machine and is not encrypted. `rex doctor` warns
  about untracked `.env*` files in snapshotted folders. Rex runs the `git`
  binary against its own shadow repository with an explicit environment
  (no system or global config, no hooks, no prompts) and never writes to the
  user's repository. Reports of a snapshot writing to the user's repo,
  running the user's hooks or config, reading or writing outside the folder
  and the store, or exposing the store to other users are in scope.
- **The Claude status-line tap** (`usage`, `usage_statusline_tap`). When on,
  rex starts Claude Code with a status line that runs
  `rex-daemon statusline-tap`. The tap reads Claude's status-line JSON from
  stdin and sends only the usage fields to the daemon (plan rate-limit
  percentages and reset times, session cost, model id and name, Claude's
  conversation id); the prompt, transcript path and folder in that JSON are
  never forwarded. It also **reads** your user-level Claude settings file and
  **runs the status line command found there** with the same input, so it
  runs code you already configured, nothing else. The daemon resolves that
  file's absolute path once per spawn from its own environment
  (`$CLAUDE_CONFIG_DIR/settings.json`, default `~/.claude/settings.json`) and
  passes it as `--user-settings`; the tap reads only that path, ignores its
  own `CLAUDE_CONFIG_DIR` and `HOME` (Claude applies the `env` of project
  settings files to what it starts, so an agent could redirect them), and
  refuses a relative path, a symlink or a non-regular file. On a signal the
  tap kills the command's whole process group. It deliberately never reads
  project-level settings (`.claude/settings.json`, `.claude/settings.local.json`
  in a session folder), nor takes a folder from Claude's JSON: an agent can
  write those files from inside its folder at any time, and Claude itself
  reads them only at startup for a trusted folder, so honoring them would let
  an agent run a command of its choosing outside Claude's permission system.
  It never writes a Claude settings file. The daemon ignores a sample for an unknown
  session. Reports of the tap sending more than those fields, running
  anything other than your own user-level status line command, or writing to your Claude
  settings are in scope. The usage ledger (`usage.jsonl`) holds session ids,
  tool and model names, token counts and cost, mode 0600, and no prompts or
  output.
- **Malformed protocol input** that crashes or hangs the daemon.
- **Resume ids reaching a command line.** A tool's conversation id (taken
  from the agent's hook, its exit text or `meta.json`) is passed to the
  agent as one argument. Rex accepts only a canonical UUID at every point it
  reads one, so a crafted value cannot add a flag or an argument. Anything
  that gets another value onto a command line is in scope.

### Out of scope

- **`~/.config/rex/init.lua` is trusted user code.** It runs inside the
  daemon with your privileges and is not sandboxed. Anyone who can edit it
  can already run code as you. The same applies to `tools.yaml` and
  `config.yaml`, which choose the programs rex spawns.
- What the agent CLIs themselves do (Claude Code, Codex, Antigravity and others),
  including the commands they run in your repository.
- Folders you list in the `auto_trust_dirs` setting or trust for one
  session (`trust_folder`). For sessions there,
  rex starts the agent with the folder trusted (Antigravity, for that
  session only) or answers its "trust this folder" dialog (Claude Code),
  so the repository's own agent settings apply without that prompt. The
  setting is empty by default, and `rex setup` sets it only when you
  answer yes or pass `-auto-trust`. A session trust request is refused for
  relative paths, paths with `..`, a filesystem root, the home folder (also
  as a case variant) and any folder that holds it, and
  Resume keeps it only while the stored folder still passes. rex trusting a
  folder you did not list or ask to trust is in scope.
- The `skip_permissions` setting. When you turn it on, rex starts Claude
  Code and Antigravity with `--dangerously-skip-permissions`, so those
  agents run commands and edit files without asking you, but only in a
  folder under `auto_trust_dirs` or trusted for the session. Elsewhere the flag is left off. It is off
  by default. rex adding that flag while the setting is off, or in a folder
  that is not trusted, is in scope.
- An attacker who already runs code as your user, or as root.
- Vulnerabilities in third-party dependencies that rex does not reach. Use
  `make vuln` (govulncheck) to check reachability first.
