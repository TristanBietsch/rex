# Medium hardening (M1–M7)

Status: spec, revised after review, waiting for approval. Plan: [medium-hardening-plan.md](medium-hardening-plan.md).

A security review of rex found two high issues (fixed: agents can no longer drive sessions through the socket, and `rex new -trust` needs a terminal) and seven medium ones. This spec fixes six of the seven and narrows the seventh (M6) to what can be done safely now. Each part ships on its own branch, with its own tests and CHANGELOG entry, and does not depend on the others.

## M1. Agents get an allowlisted environment

**Problem.** Every agent, model probe and loop check inherits the daemon's whole environment, frozen when the daemon started (`pty/supervisor.go:175-177` starts from `os.Environ()`; `daemonctl/daemon.go:90` and `server/hot.go:363` pass the caller's environment on). An API key or token exported in the shell that started rex reaches every agent and everything an agent runs, for days. On Linux it is also readable from the daemon's own `/proc/<pid>/environ` by any process of the same user, agents included.

**Behavior.**
- A new setting `child_env` (`allowlist` | `inherit`, default `allowlist`) picks the policy. `inherit` is today's behavior, kept as an escape hatch.
- In `allowlist` mode a child gets only:
  1. the base names for its OS (below);
  2. the names its tool declares in a new registry field `env_pass` (built-in tools declare their own auth and provider variables, e.g. Claude: `ANTHROPIC_*`, `CLAUDE_CODE_USE_BEDROCK`, `CLAUDE_CODE_USE_VERTEX`, `CLAUDE_CODE_OAUTH_TOKEN`, `CLAUDE_CODE_GIT_BASH_PATH`, `AWS_*`, `ANTHROPIC_VERTEX_PROJECT_ID`, `CLOUD_ML_REGION`, `GOOGLE_APPLICATION_CREDENTIALS`; each list checked against the tool's current docs); a user's `tools.yaml` can add more;
  3. the names in a new setting `env_passthrough` (comma-separated, default empty), for every tool;
  4. rex's own additions (`REX_HOOK_FILE`, a tool's `status_env`), as today.
- Always removed, in both modes, because they mark a *running* Claude session and make a nested Claude misbehave: `CLAUDECODE`, `CLAUDE_CODE_ENTRYPOINT`, `CLAUDE_CODE_SSE_PORT`. Nothing else under `CLAUDE_CODE_*` is removed by name. The OSC 7501 drop list (`TMUX`, `TERM_PROGRAM`, …) stays as it is.
- Base names (a trailing `*` matches a prefix):
  - all: `PATH HOME USER LOGNAME SHELL TERM COLORTERM LANG LC_* TZ TMPDIR XDG_* HTTP_PROXY HTTPS_PROXY NO_PROXY ALL_PROXY http_proxy https_proxy no_proxy all_proxy SSL_CERT_FILE SSL_CERT_DIR NODE_EXTRA_CA_CERTS EDITOR VISUAL`
  - plus `SSH_AUTH_SOCK` while the new setting `env_ssh_agent` is `true` (default)
  - macOS adds `__CF_USER_TEXT_ENCODING`
  - Linux adds `DISPLAY WAYLAND_DISPLAY DBUS_SESSION_BUS_ADDRESS`
  - Windows adds `SystemRoot SystemDrive windir COMSPEC PATHEXT USERPROFILE USERNAME APPDATA LOCALAPPDATA ProgramData ProgramFiles ProgramFiles(x86) ProgramW6432 CommonProgramFiles TEMP TMP HOMEDRIVE HOMEPATH NUMBER_OF_PROCESSORS PROCESSOR_ARCHITECTURE OS` (matched without case, as Windows does)
- What the base list gives away, stated in the docs: `SSH_AUTH_SOCK` lets an agent use every key loaded in the SSH agent for as long as it runs (turn it off with `env_ssh_agent: false`); a proxy URL can carry a user name and password.
- **The daemon's own environment is filtered too.** At startup, a daemon whose environment is not yet filtered re-executes itself (same pid, `syscall.Exec`) with: the base names, the union of every tool's `env_pass`, and `env_passthrough`. Hot restarts pass that environment on unchanged. On Windows, where exec-in-place does not exist, `daemonctl.Start` builds the same environment before it starts the daemon.
- The same policy applies to model probes (`registry/models.go:155`, with the probed tool's `env_pass`) and loop checks (`loop/check.go:77`, base names and `env_passthrough` only).
- `child_env`, `env_passthrough` and `env_ssh_agent` are trust settings: changing them from the CLI works only at a terminal, like `skip_permissions`.
- Each spawn logs, at Debug, how many inherited variables were dropped (never names or values).
- **Remaining exposure, stated in SECURITY.md:** on Linux a process can read another same-user process's environment, so an agent can still read the keys of the tools it can see running (its own and sibling agents'). The daemon no longer holds anything else.

## M2. The first prompt is pasted, not put on argv

**Problem.** Built-in tools other than ollama put the first prompt on the command line (`builtin.yaml`: claude `["--","{prompt}"]`, codex, agy/gemini/qwen `--prompt-interactive=`, cursor, grok). The process table shows it to every local process, and any tool that records command lines keeps it. rex's own logs redact it; the OS does not.

**Behavior.**
- Claude Code is switched to the existing paste path (`prompt_paste`, bracketed paste, then Enter). Today the paste path cannot reach Claude, because Claude's adapter never reports ready (`adapter/claude_hooks.go:702`).
- `prompt_paste` gains a field `ready_regex` (inside `prompt_paste`, so a `tools.yaml` that switches a tool back to `prompt_args` replaces both together). The paste goroutine (`pty/supervisor.go:354-414`) types the prompt only when:
  1. the adapter, or `ready_regex`, says the agent is ready, **and**
  2. the tool's trust prompt (`trust_prompt.shown`) is not on screen, **and**
  3. the screen has been idle for `settle_ms`.
  Both regexes are matched against the **rendered screen** (the vt10x emulator's text, as the trust answerer already does at `supervisor.go:789`), not the raw output window, so a dialog that has closed no longer counts. Condition 2 matters: an Enter typed into Claude's "trust this folder?" dialog would answer it.
- The 30 s deadline counts only time when no dialog is on screen: it is paused while the trust prompt shows, so a user who answers the dialog minutes later still gets the prompt delivered. A hard cap of 10 minutes bounds the wait (a hot restart is refused while a prompt is pending).
- If the prompt is not delivered, the session's card says so ("first prompt not delivered; paste it yourself"), and the prompt stays in the audit record as today.
- The other argv tools keep argv until each gets a verified `ready_regex`. That is follow-up work, one tool per change, listed in `TODO.md`.

## M3. A record of who drove a session

**Problem.** The daemon does not log who connected or what they asked for (`SendInput` is not logged at all; connections are Debug lines), so it cannot say whether input came from the user's board, a script, or something else.

**Behavior.**
- The daemon reads the connecting process's executable path at accept: `/proc/<pid>/exe` on Linux (kernel-verified), `kern.procargs2` on macOS (the path as given to exec, possibly relative or through a symlink, and set by the process itself, so logged as reported, not verified), nothing on Windows.
- At the first intent on a connection (so liveness probes that send nothing stay out of the log) it logs at Info: connection id, pid, executable, `in_session`, and the `Hello` client name if one was sent.
- **Every intent is logged at Info** with the connection id, intent type and session id, except the read-only ones: `Hello`, `Subscribe`, `Resize`, `Boards`, `UsageQuery`, `FocusFilter`, `UsageSample`. A new intent is therefore logged unless someone adds it to that list.
- Input (`SendInput`, `Reply`): the first input from a connection to a session is logged at Info; when the connection ends, one Info line per session gives the byte count. Never the bytes themselves.
- Audit:
  - The requester (`client`, `pid`, `exe`) is stored in the session's `meta.json` at spawn, whatever the prompt, and the audit record written at the session's end carries it as `requested_by`.
  - A delete appends its own audit line (`event: "delete"`, `session_id`, `deleted_by`, time), because the session's record is normally written once, when it finishes, before any delete.

## M4. The summarizer talks only to a local Ollama by default

**Problem.** `boot/ollama.go:81-90` turns any `OLLAMA_HOST` into an `http://` URL with no checks: a remote host gets the last 2 KiB of every session's screen in cleartext, `0.0.0.0` becomes port 80, and redirects re-send the request body (`summarizer/client.go:56-75`). Replies are read with no size limit.

**Behavior.**
- `OLLAMA_HOST` is parsed as Ollama itself accepts it: a bare IP (`::1`, `::`, `0.0.0.0`) is checked with `net.ParseIP` before anything else; `host:port` and `:port` forms are accepted; a full URL is parsed with `net/url`. No scheme means `http`; no port means `11434`; an empty or unspecified host means `127.0.0.1`. Credentials in the URL are refused.
- A host that is not loopback (`127.0.0.0/8`, `::1`, `localhost`) is refused unless the new setting `summary_allow_remote` is `true` **and** the scheme is `https`. A refused endpoint turns the summarizer off with a health message that names the reason; the daemon keeps running.
- The client follows no redirects and reads at most 1 MiB of any reply.
- `rex doctor` gains a "summarizer endpoint" row: the URL as resolved from this shell's `OLLAMA_HOST`, and whether it is local, remote (allowed) or refused.
- `summary_allow_remote` is a trust setting.

## M5. Terminal escapes from agents are filtered

**Problem.** Agent output reaches the user's terminal with dangerous sequences intact: the TUI attach popup prints C1 control runes (U+0080–U+009F) that vt10x stores in cells (`tui/attach_render.go:154`); `rex attach` is a raw pipe, transcript replay included (`cli/session/attach.go:195`); `rex log` passes C0 and C1 controls (`cli/inspect/ansi.go:82-88`). In some terminals that is enough to set the clipboard (OSC 52), the window title, or hyperlinks.

**Behavior.**
- A new filter (package `termfilter`) passes text, C0 `\t \n \r \b` and SO/SI (`0x0E`, `0x0F`, used for curses line drawing under `TERM=screen*` and `tmux*`), every CSI sequence (colors, cursor movement, modes) and short ESC sequences (`ESC 7`, `ESC 8`, `ESC =`, `ESC >`, charset selection).
- It drops, whole, OSC, DCS, APC, PM and SOS strings introduced by 7-bit `ESC ]`, `ESC P`, `ESC _`, `ESC ^`, `ESC X` or by the **UTF-8-encoded** C1 runes U+009D, U+0090, U+009F, U+009E, U+0098. A string ends at ST (`ESC \` or U+009C), BEL, CAN or SUB, or at an `ESC` followed by anything other than `\` (that `ESC` then starts the next sequence, as terminals do). A string body longer than 64 KiB is abandoned: the filter drops what it held and treats the following bytes as text.
- A **raw** byte 0x80–0x9F that is not part of valid UTF-8 is one invalid byte and is dropped on its own; it never starts a string (in a UTF-8 terminal it means nothing, and in Windows-1252 text it is an ordinary letter such as `ž`).
- Other C0 controls, C1 runes and bidi controls U+202A–U+202E and U+2066–U+2069 are dropped.
- It keeps state across writes, so a sequence split between two chunks is handled the same as in one write.
- `rex attach` writes replay and live output through it. On detach it also resets cursor shape (`CSI 0 SP q`), synchronized output (`CSI ? 2026 l`), modifyOtherKeys (`CSI > 4 ; 0 m`) and the kitty keyboard stack (`CSI < u`), in addition to what `exitAlt` resets today.
- The TUI popup writes a space for any cell rune in U+0080–U+009F or a bidi control.
- `rex log` (without `-bytes`) drops C0 other than `\t` and `\n`, drops `\r`, drops C1 runes and bidi controls, and replaces invalid UTF-8 with U+FFFD.
- Cost: links (OSC 8) and titles set by agents no longer reach the terminal through `rex attach`.

## M6. Releases: tags must be on master; signing gets its own spec

**Problem.** `rex update` and the installers check `SHA256SUMS` from the same release as the archive (`selfupdate/selfupdate.go:247-290`, `install.sh:226-244`). Whoever controls the GitHub account or a release can replace both. Any `v*` tag on any commit triggers a release (`release.yml:12-14`). macOS binaries are not signed.

**Behavior now.**
- The release workflow refuses a tag whose commit is not on `master`.
- `SECURITY.md` and `README.md` say plainly what a release check proves today (the download matches what GitHub published, nothing more) and keep the manual `gh attestation verify` step.

**Deferred to its own spec**, because a signature made from what CI built does not help against the threat it is meant for (someone who controls the account or CI controls what gets built, and the maintainer would sign it):
- the maintainer rebuilds each release from the tag on their own machine and compares hashes before signing (Linux builds use cgo, so this needs a pinned toolchain or container);
- signatures in SSHSIG format (`ssh-keygen -Y sign` / `-Y verify`), which every supported OS ships, rather than raw Ed25519 (macOS's LibreSSL `openssl` cannot verify those);
- `rex update` and the installers verify it; Apple Developer ID signing and notarization; GitHub tag-protection rules.

## M7. `rex uninstall -all` removes everything rex wrote

**Problem.** `-all` removes `~/.local/state/rex`, the sessions folder and `~/.config/rex` (`setup/uninstall.go:154-156, 319-323`) but leaves the rest of `~/.local/share/rex` (audit trail, usage ledger, snapshots with copies of repository files, exports with transcripts, loops, workspace lists), `~/.cache/rex`, logs under `REX_LOG_DIR`, and each workspace's `.rex/*.jsonl`.

**Behavior.**
- `-all` also removes `DataDir` and `CacheDir` whole, the rex log files under `REX_LOG_DIR` (only files named like rex's logs), and in each workspace listed in `workspaces.json` the rex-written files `.rex/sessions.jsonl` and `.rex/audit.jsonl`. It reads `workspaces.json` before it removes `DataDir`.
- It never removes: a workspace's `.rex/config.lua` or `.rex/workspace.json` (the user may have committed them), or the shell-profile backups `*.rex-backup-*` (they hold the user's own files). It prints each of these paths at the end as "kept".
- Safety: every path removed must be inside the user's home or a listed workspace's `.rex/`, and is checked with `Lstat`; a symlink is not followed or removed. `-dry-run` lists all of it.
- The closing hint and the `-wipe-state` help list the real folders.

## Decisions

| # | Decision | Recommendation |
|---|---|---|
| D1 | M1 is on by default (`child_env: allowlist`), a breaking change for anyone who relied on inherited variables | Yes, with a CHANGELOG migration note and `inherit` as the escape hatch |
| D2 | Built-in tools pass their own auth and provider variables (Bedrock, Vertex, git-bash path included) to themselves only; only the three running-session markers are always removed | Yes |
| D3 | `SSH_AUTH_SOCK` and proxy / CA variables are in the base list; `SSH_AUTH_SOCK` can be turned off with `env_ssh_agent` | Yes. Agents need git over SSH and proxies, but the docs say what this gives away |
| D4 | M2 covers Claude now, with the deadline paused while a dialog is on screen; other tools keep argv until each has a verified `ready_regex` | Yes |
| D5 | A remote summarizer needs `summary_allow_remote: true` and https | Yes |
| D6 | `rex attach` drops all OSC strings, so agent hyperlinks and titles are lost there | Yes; the TUI popup never showed them either |
| D7 | M6 is only the tag-on-master check and honest docs now; release signing moves to its own spec | Yes: signing what CI built would not stop the attack it is for |
| D8 | Apple notarization and tag-protection settings are in that later spec | Yes |
| D9 | Order: M4, M5, M7, M3, M1, M2, M6 | Yes: small and self-contained first; M1 and M2 change how agents start |
| D10 | The daemon filters its own environment at startup (re-exec in place) | Yes: otherwise on Linux an agent reads every secret from the daemon's `/proc/<pid>/environ` |
