# Workspaces and the audit trail

A workspace is a folder that has a `.rex/` folder in it, like Cursor's `.cursor/`. rex gives the workspace a short id, a name and a color, and keeps a history of its finished work there.

Workspaces are off by default. Turn them on in Settings → Features → `workspaces`, or with `rex config set workspaces true`. Off means inert: rex never looks for or writes a `.rex/` folder, the daemon does not look for a workspace when a session starts, and `rex ws` and `rex here` exit 5.

The audit trail is on by default (`audit_trail`) and works with or without workspaces.

**Built so far:** workspace identity (`rex ws`), the audit trail (`rex audit`), sessions tagged with their workspace, the session index, the workspace board (`rex here`) and per-workspace settings in `.rex/config.lua` (behind `rex ws trust`).

## Start one

```sh
cd ~/dev/myproject
rex ws init --name myproject
```

This creates:

```
myproject/.rex/
├── workspace.json   # id, name, color
├── sessions.jsonl   # one line per session that ran here (see "Sessions and the workspace board")
├── audit.jsonl      # finished work, written by the daemon (see "Audit trail")
└── config.lua       # optional, you write it: workspace settings (see "Workspace settings")
```

`rex ws init` also appends two lines to the repository's `.git/info/exclude` (never to a tracked `.gitignore`): `/.rex/`, and `/.rex/audit.jsonl` so that even an un-ignored `.rex/` never commits audits. Git then ignores `.rex/` on your machine only. Inside a linked git worktree the shared exclude file is used.

To share the workspace id with your team, commit **only** `workspace.json` (`git add -f .rex/workspace.json`) and, if you like, `config.lua`, never `audit.jsonl`: it holds prompt lines and, with `audit_full_prompt`, whole prompts.

That write into `.git` is fenced in, because a repository you cloned is untrusted: for a worktree `.git` file rex requires the git directory it names to point back at that file and to hold a `HEAD`; a `commondir` must be an ancestor of that directory (the `.git/worktrees/<name>` layout); `.git/info` and `info/exclude` must not be symlinks and `info` must stay inside the git directory; the file must be a regular file you own and under 1 MiB. Anything else makes `rex ws init` fail with `unsafe .rex` and write nothing. (Submodule `.git` files have no such back-link and are refused too.)

Running `rex ws init` in a folder that is already a workspace keeps its id (and renames it if you pass `--name`). `$HOME` and `/` are refused.

## Ids and colors

Each workspace gets an 8-character id (for example `7f3b89a2`) when it is created, and the first color from a colorblind-safe palette (Okabe-Ito) that no known workspace uses.

```sh
rex ws id                       # the workspace around this folder
rex ws ls                       # every workspace rex has seen (from ~/.local/share/rex/workspaces.json)
rex ws rename 7f3b89a2 backend  # an id, or any folder inside the workspace
rex ws color 7f3b89a2 3         # a swatch number 1-7 ...
rex ws color . '#D55E00'        # ... or #RRGGBB
```

Anywhere a workspace is expected you can give its id or a folder inside it. All of these take `-o text|txt|md|json|agent`.

The nearest `.rex/` wins: with `~/dev/.rex` and `~/dev/rex/.rex`, a session in `~/dev/rex` belongs to the inner one. Discovery stops before your home folder and before `/`, so neither can be a workspace.

If you move a workspace folder, its id goes with it (the id lives in `workspace.json`). Run `rex ws id` inside the new location and rex updates its registry.

## Sessions and the workspace board

With `workspaces` on, the **daemon** tags every session it starts with the id of the nearest workspace above the session's folder (`workspace_id` on the session, kept across daemon restarts). A client cannot choose it: a `workspace_id` sent when starting a session is ignored, so a session cannot be filed into another workspace. Sessions started before you turned the feature on, outside any workspace, or with `workspaces` off have no workspace id.

```sh
cd ~/dev/myproject/src
rex here                 # the board for the workspace around this folder
rex ws open ~/dev/other  # the same for another folder (any folder inside the workspace)
```

`rex here` opens the usual board with a fixed filter: only that workspace's sessions are listed, counted in the header, cleared by `:prune`, shown in the overview and the loops overlay, matched by session selectors and announced by sounds. No key or command changes the filter; open `rex` for the whole board. What changes on screen:

- The header starts with `[7f3b89a2 myproject]` in the workspace color (the id alone in a narrow layout).
- The window border takes the workspace color. On a workspace board the border is **on by default**, so the color shows; set `window_border` yourself (to `false` to switch it off) or use `:border` for one run and rex follows. An explicit `window_border_color` or `:border <color>` also wins over the workspace color.
- The new-session wizard and quick spawn (`i`) start in the **workspace root**, not in the folder you ran `rex here` from.

With no `.rex/` above the folder, `rex here` exits 5 with ``no .rex/ here — run `rex ws init` ``. With `workspaces` off it exits 5 with the usual `off: enable in Settings → Features (rex config set workspaces true)`. Both need a terminal (like `rex`): `-o`, `--agent` and a pipe are refused.

If the daemon does not tag sessions (it is older than rex, or still running with `workspaces` off), the board cannot tell which sessions belong to the workspace, so it shows **every** session and says once: ``daemon doesn't tag workspaces — `rex daemon restart` ``. The daemon says that it tags in its snapshot (`workspace_tagging`), so a new daemon with no workspace session yet still scopes the board.

### The session index

The daemon also keeps `<workspace>/.rex/sessions.jsonl`: one line when a session starts and another when it first reaches a terminal state (a resumed session adds a line at its next end). A reader keeps the newest line per `session_id`.

```json
{"session_id":"…","short_id":"7d4f","tool":"claude","model":"opus","name":"fix-auth","cwd":"/…","started_at":"…","ended_at":"…","state":"done","resume_id":"…"}
```

`ended_at` is absent on the spawn line. The file is `0600`, opened append-only without following symlinks; a symlink, a hard link, another user's file, an unsafe `.rex/` or a read-only folder does not stop the session: the daemon logs one warning for that file per run and keeps going. The index holds no prompt text. It is not a secret, but like `audit.jsonl` it is excluded from git by `rex ws init` (the whole `/.rex/` is).

## Workspace settings

A workspace can carry its own `.rex/config.lua`: the same Lua as `~/.config/rex/config.lua` ([howto/lua-config.md](howto/lua-config.md)), applied on top of it while rex runs inside the workspace.

```lua
-- ~/dev/myproject/.rex/config.lua
rex.set("theme", "paper")
rex.set("row_density", "compact")
```

Precedence, lowest first: registry defaults, `config.yaml`, your `config.lua`, the workspace's `.rex/config.lua`.

**Where it applies.** In rex commands run from a folder inside the workspace (`rex config list`, `rex new`, `rex doctor`, ...), and on the workspace board (`rex here`, `rex ws open <path>`, which use the opened workspace wherever you start them). Never on the system-wide board (`rex` with no command) and never in the daemon: the daemon keeps your own settings for every session.

**Nothing runs until you trust it.** A repository you clone can ship a `.rex/config.lua`. rex does not read it, let alone run it, until you trust that workspace:

```sh
cat .rex/config.lua      # read it first
rex ws trust             # the workspace around this folder (or: rex ws trust <folder>)
rex ws trust -rm         # stop trusting it (also: rex ws trust -rm <id|folder>)
```

Until then rex shows one notice where `config.lua` problems show (`rex config list`, `rex doctor`, the TUI's boot row): `<folder>/.rex/config.lua: not applied: workspace not trusted`.

Trust is kept in `~/.local/share/rex/workspace-trust.json`, outside every `.rex/`, so a repository cannot ship it. It is bound to the **real folder** (symlinks resolved) **and the workspace id** together: the same id in another folder (a copy of a committed `workspace.json`), a different id in the trusted folder, or the workspace moved to a new folder are all untrusted until you run `rex ws trust` again. Trust also covers the **exact bytes** of `.rex/config.lua` you trusted (its SHA-256): after any edit (yours, a `git pull`, an agent) the file is not run, with the notice `changed since you trusted it`, until you read it and run `rex ws trust` again. A workspace trusted with no `config.lua` does not run one added later either. The hash is checked on the very bytes that are evaluated.

`rex ws trust` asks before it trusts (`[y/N]`, showing the file's size and hash) and only at a terminal: in agent mode, with `-o`, or with stdin or stderr not a terminal it refuses (exit 6), and no flag skips the question, so an agent working in the repository cannot trust its own `config.lua`. Re-running it on an unchanged, already trusted file asks nothing and writes nothing. `rex ws trust` and `rex ws trust -rm` lock `workspace-trust.json` while they rewrite it, so two at once never lose a change.

**What it may set**, and nothing else: display settings, `theme`, `spinner`, `waiting_spinner`, `row_density`, `prompt_glyph`, `reduce_motion`, `blinking_enabled`, `show_help_bar`, `header_style`, `window_border`, `window_border_color`, `column_layout`, `mobile_layout`, `mouse_enabled`, `attach_scrollback_lines`, `screensaver_enabled`, `screensaver_after_minutes`, `board_filter`, `search`, `timestamps`, `summary_preview`, `waiting_section`, `usage_meter`, `summary_detail`, `desc_animation` and `attach_banner`. Every other setting is refused: among them `skip_permissions` and `auto_trust_dirs` (trust), the quick-spawn tool, model and effort and `extra_agents` (what runs), `audit_trail` and `audit_full_prompt`, sounds, `retire_after_hours`, features that run commands or write files, and the switches `workspaces`, `lua_config` and `live_reload`. A setting added to rex later is refused until it is allowed on purpose. Each refused `rex.set` is ignored with a warning (`"auto_trust_dirs" cannot be set from Lua (ignored)`) and leaves your value alone. Workspace files also cannot `require` modules.

**When it fails.** A trusted `.rex/config.lua` that is refused (see below), has a syntax or runtime error, or runs out of time keeps its last good values in that process and puts `skip_permissions` and `auto_trust_dirs` back to their defaults until it runs again, as a broken `~/.config/rex/config.lua` does.

**Where it shows.** A value the workspace sets is locked like any Lua value:

- `rex config list` shows its source as `workspace /path/to/myproject/.rex/config.lua:2`; `-o json` has it in `set_at`.
- `rex config set` and `reset` on it exit 5 naming that file and line.
- The settings page marks the row `ws` (your own `config.lua` rows say `lua`), the detail line says `set in <file>:<line>`, and `e` opens that file.
- `rex doctor` has a `workspace config.lua` row: not trusted, failed (with the reason), its first warning, or each setting it sets with its line.

`workspaces` and `lua_config` both gate it: with either off, rex never looks at `.rex/config.lua`.

## Audit trail

When a session finishes, the daemon writes one line to the audit trail: who ran what, when, for how long, and how it ended. A session that is cleared (`rex rm`, `rex prune`, or the done timer) before its record was written gets it first, so a session is never deleted without one. A resumed session writes a second record when it ends again.

| Where the session ran | File |
|---|---|
| inside a workspace (`workspaces` on) | `<workspace>/.rex/audit.jsonl` |
| anywhere else, or `workspaces` off | `~/.local/share/rex/audit.jsonl` |

If a workspace's file cannot be written (read-only folder, a symlink in the way), the record goes to the global file instead, and the daemon logs one warning for that file per run.

```sh
rex audit                          # this workspace (or the global trail outside one)
rex audit --all --since 7d         # global + every known workspace
rex audit --ws 7f3b89a2 --state failed
rex audit --tool claude -o json
```

`--ws` takes an id or a folder and needs `workspaces` on. With `workspaces` off only the global trail is read. `--since` takes `7d`, `12h`, `90m` or a date (`2026-11-01`); `--state` is `done`, `failed` or `crashed`; `--tool` is a tool id. Newest first.

Each record has: `ts`, `workspace_id`, `session_id`, `short_id`, `name`, `tool`, `model`, `prompt_first_line`, `prompt` (only when opted in), `state`, `started_at`, `ended_at`, `duration_s`, `resume_id`, `description`.

### What a record holds

By default a record keeps only the **first line** of the initial prompt (200 characters at most, control characters removed). Turn on `audit_full_prompt` to keep the whole prompt, capped at 20,000 characters; it applies to sessions started afterwards, and `rex audit` shows it only in `-o json`. **Prompts can contain secrets**, and a workspace's `audit.jsonl` may sit in a synced or committed folder. The prompt is held in the daemon's memory only until the session ends; a daemon restart forgets it, so such a session's record has no prompt.

The files are `0600`. Reading skips a torn last line (a crash, or two machines appending to one synced file) and drops duplicates (same `session_id` and `ended_at`).

## Moving between machines

rex has no sync of its own. `.rex/` lives in your project folder, so it moves with the folder: through git (if you commit it), Syncthing, iCloud Drive or a plain copy. The audit files merge cleanly when two machines append to one file. Transcripts stay in rex's data folder (`~/.local/share/rex`) and do not travel.

## Safety

- rex never follows a symlinked `.rex/`, `workspace.json`, `sessions.jsonl` or `audit.jsonl`, and refuses a `.rex/` that other users can write to or that another user owns (error: `unsafe .rex`, exit 6 on the CLI).
- `workspace.json` is read as data only: files over 64 KiB are rejected, the name is cut to 40 characters and stripped of control characters, a color that is not `#RRGGBB` is dropped, and an id that is not 8 hex digits is refused. Nothing in it changes how rex behaves.
- Every file rex writes under `.rex/` is readable by you only (`0600`, folders `0700`).
- A repository cloned from the internet can ship a `.rex/`. Opening it shows its workspace name and id, nothing more. Its `.rex/config.lua` is not read until you run `rex ws trust`, and even then it may set only the display settings listed under "Workspace settings", and runs only while its bytes match the ones you trusted.
- `.rex/config.lua` is read with the strictest checks: it is opened once without following a symlink (even one to a file of yours), and the opened file must be a regular file you own, not group- or world-writable, without other hard links, and at most 256 KiB. The `.rex/` around it must pass the checks above. A refused file fails the trust settings closed (see "When it fails").
- `rex ws init` refuses `$HOME` and `/`.
- `rex ws init` writes outside `.rex/` once: two lines in `.git/info/exclude`, under the checks described above.
- `rex audit` and the daemon read at most the newest 8 MiB of an audit file; `rex audit` says when older records were left out.
- A session cleared before it ended (or still running) is recorded as `failed` with the note `deleted while running`. If its record cannot be written, the session is kept, an error is logged, and the next sweep or delete tries again.
