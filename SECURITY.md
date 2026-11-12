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
| `v2.0.0` (current release) | Yes |
| `v1` | No |
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
- **Theme files.** `~/.config/rex/themes/*.yaml` are treated as untrusted
  data, since people share them. They hold colors and one-character
  glyphs, never code. rex validates every color (`#RGB`, `#RRGGBB`, an ANSI
  index or empty) and every glyph (exactly one printable character, one cell
  wide, no control, escape, bidirectional or zero-width characters),
  caps a file at 64 KiB and the directory at 64 files, and on macOS and
  Linux skips a file or directory that another user owns or that group or
  others can write. A symlink is followed (a dotfiles manager may link themes
  in), but its target must be a regular file that passes those same checks,
  made on the opened file so a link swapped in between check and read cannot
  skip them; dangling links, FIFOs, devices and directories are skipped. On
  Windows a link must stay inside the themes directory. A bad file is skipped
  with a reason and never stops rex. A theme file that puts bytes on your
  terminal other than through those checks is in scope. On Windows the
  ownership and mode check does not apply (ACLs are out of scope).
- **The CLI error log.** Failed `rex` commands are recorded in
  `~/.local/state/rex/cli-errors.log` (0600, 1 MiB plus one backup). It stores
  structure only: the command and subcommand when rex knows them, the names
  (never values) of the flags the command defines, argument counts, output
  format, terminal state, exit code and kind, a code from a fixed list, and
  the Go types of the error chain. It never stores error or hint text,
  argument or flag values, selectors, prompts, `config set` values, paths,
  stdin, environment values or transcripts, because free text cannot be
  scrubbed reliably. Any such content appearing in it is in scope. Turn it
  off with `cli_error_log: false` or `REX_ERROR_LOG=0`.
- **Loop files and what they run.** With the `loops` feature on, the daemon
  runs the `check` command of a loop file after each agent turn. The file
  is trusted because you wrote it: only `~/.config/rex/loops/<name>.yaml` is
  read, never a path from a client or text from an agent, and a symlink, a
  file another user owns and a group- or world-writable file are refused.
  The command is an argument list run directly (no shell), in its own
  process group, with a timeout and capped output. A loop never answers an
  agent's permission or question prompt: it runs only for agents that report
  why they wait (Claude Code's hooks today, never a screen-reading guess),
  sends only when the session is re-read at a finished turn with no question,
  pauses and waits for you at any prompt, and holds its prompts back while
  you type (its send and your keystrokes share a per-session lock). Loop
  sessions run under the daemon's context, so turning the feature off pauses
  loops without killing agents. Anything that makes the daemon run
  a command that did not come from such a file, answer a prompt on its own,
  or read a loop file from elsewhere is in scope. Loop state under
  `~/.local/share/rex/loops` is owner-only.
- **The audit trail and workspace folders.** With `audit_trail` on (the
  default) the daemon appends one line per finished session to
  `~/.local/share/rex/audit.jsonl`, or to `<workspace>/.rex/audit.jsonl` for
  a session inside a workspace. A line holds ids, names, the tool, times, the
  end state and the first line of the initial prompt (200 characters,
  control characters removed). With `audit_full_prompt` (off by default) it
  also holds the whole prompt, which can contain secrets, in a file that may
  sit in a synced or committed folder. rex writes these files 0600 in 0700
  folders and never follows a symlink at `.rex/`, `workspace.json` or
  `audit.jsonl`. It refuses a `.rex/` that is group- or world-writable or
  owned by another user. A repository can ship a `.rex/`: `workspace.json`
  is read as data only (64 KiB cap, name and color validated) and nothing in
  it changes rex's behavior. Reports of rex writing audit or workspace files
  through a symlink, outside those locations or with a wider mode, of
  `.rex/` content changing behavior, or of the prompt appearing in a record
  without `audit_full_prompt` are in scope. `rex ws init` also appends two
  lines to the repository's `.git/info/exclude`. A cloned repository is
  untrusted input to that write: rex requires a worktree's git directory to
  point back at the `.git` file and hold a `HEAD`, a `commondir` to be an
  ancestor of it, `info/` and `info/exclude` to be real (non-symlink) paths
  under the git directory, and the file to be a regular file you own under
  1 MiB; otherwise it refuses and writes nothing. A way to make rex write
  anywhere else through a crafted `.git` is in scope.
- **`config.lua`.** `~/.config/rex/config.lua` and the modules it requires
  from `~/.config/rex/lua/` run in every rex process that reads settings
  (TUI, daemon, CLI) while `lua_config` is on. rex refuses a file or folder
  another user owns or that group or others can write, on the opened file
  (a symlink only to a target that passes), and every string it accepts from
  Lua is checked for control, escape, bidirectional and zero-width
  characters; errors and warnings are flattened before display. A
  `config.lua` owned by another user being evaluated, or bytes from
  `config.lua` reaching the terminal unflattened, are in scope.
- **A workspace's `.rex/config.lua`** is untrusted input: a cloned
  repository can ship one. rex never reads it before `rex ws trust` (trust
  lives in `~/.local/share/rex/workspace-trust.json`, outside every `.rex/`,
  bound to the real folder, the workspace id and the SHA-256 of the file
  you trusted; `rex ws trust` shows the file, flattened, with its size and
  SHA-256, asks at a terminal and refuses agents and pipes), opens it without following a symlink, and checks the opened file
  (yours, not group- or world-writable, one hard link, 256 KiB). Even
  trusted it runs with no `require` and may set only an allow-list of
  display settings. Running an untrusted or edited-since-trusted workspace
  file, a workspace file setting anything outside the allow-list, or a
  crafted repository getting itself trusted are in scope. What a trusted
  workspace sets among the allowed display settings is not. On Windows
  the owner, mode and hard-link checks do not apply (ACLs are out of
  scope, as for `config.lua`).
- **Malformed protocol input** that crashes or hangs the daemon.
- **Resume ids reaching a command line.** A tool's conversation id (taken
  from the agent's hook, its exit text or `meta.json`) is passed to the
  agent as one argument. Rex accepts only a canonical UUID at every point it
  reads one, so a crafted value cannot add a flag or an argument. Anything
  that gets another value onto a command line is in scope.

### Out of scope

- **`~/.config/rex/init.lua` is trusted user code.** It runs inside the
  daemon with your privileges and is not sandboxed. Anyone who can edit it
  can already run code as you. The daemon refuses an `init.lua` or folder
  another user owns or that group or others can write (the `config.lua`
  rule), and logs why; running such a file anyway is in scope. The same applies to `tools.yaml` and
  `config.yaml`, which choose the programs rex spawns.
- **`config.lua` and `~/.config/rex/lua/` are trusted user code** too. Their
  restricted Lua state (no `os`, `io`, `load` or `print`; a 250 ms limit,
  size caps and a memory watchdog) is for liveness and for keeping Lua text
  off your terminal, not a security boundary. A script that sets
  `auto_trust_dirs` or `skip_permissions` grants that trust, as the same
  line in `config.yaml` would.
- What the agent CLIs themselves do (Claude Code, Codex, Antigravity and others),
  including the commands they run in your repository.
- Folders you list in the `auto_trust_dirs` setting (`rex trust`, `:trust`,
  the wizard's ctrl+a) or trust for one session (`trust_folder`: `rex new
  -trust`, the wizard's ctrl+t). The folder those offer is the repo's own
  folder, never its parent. Every CLI path that changes a trust setting
  (`skip_permissions`, `auto_trust_dirs`) works only at a terminal: adding
  with `rex trust`, `rex config set` and `reset`, and `rex setup
  -auto-trust` (agent mode, `-o` and pipes exit 6, `needs_terminal`). This
  is a speed bump for agents, not a boundary, since anything running as you
  can edit `config.yaml` or `config.lua` directly. For sessions there,
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
  that is not trusted, is in scope. Changes to `skip_permissions` and
  `auto_trust_dirs` made through rex apply to the running daemon at once; hand
  edits of `config.yaml` apply within 5 s (or on `rex reload` with
  `live_reload` off).
- An attacker who already runs code as your user, or as root.
- Vulnerabilities in third-party dependencies that rex does not reach. Use
  `make vuln` (govulncheck) to check reachability first.
