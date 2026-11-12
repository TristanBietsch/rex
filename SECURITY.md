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
  that is not trusted, is in scope. Changes to `skip_permissions` and
  `auto_trust_dirs` made through rex apply to the running daemon at once; hand
  edits of `config.yaml` apply within 5 s (or on `rex reload` with
  `live_reload` off).
- An attacker who already runs code as your user, or as root.
- Vulnerabilities in third-party dependencies that rex does not reach. Use
  `make vuln` (govulncheck) to check reachability first.
