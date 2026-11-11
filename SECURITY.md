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
