# Workspaces and the audit trail

A workspace is a folder that has a `.rex/` folder in it, like Cursor's `.cursor/`. rex gives the workspace a short id, a name and a color, and keeps a history of its finished work there.

Workspaces are off by default. Turn them on in Settings → Features → `workspaces`, or with `rex config set workspaces true`. Off means inert: rex never looks for or writes a `.rex/` folder, and `rex ws` exits 5.

The audit trail is on by default (`audit_trail`) and works with or without workspaces.

**Built so far:** workspace identity (`rex ws`) and the audit trail (`rex audit`). **Planned:** tagging sessions with their workspace, `rex here` and the workspace board, and a per-workspace `config.lua`. Nothing below describes those yet.

## Start one

```sh
cd ~/dev/myproject
rex ws init --name myproject
```

This creates:

```
myproject/.rex/
├── workspace.json   # id, name, color
└── audit.jsonl      # finished work, written by the daemon (see "Audit trail")
```

`rex ws init` also appends two lines to the repository's `.git/info/exclude` (never to a tracked `.gitignore`): `/.rex/`, and `/.rex/audit.jsonl` so that even an un-ignored `.rex/` never commits audits. Git then ignores `.rex/` on your machine only. Inside a linked git worktree the shared exclude file is used.

To share the workspace id with your team, commit **only** `workspace.json` (`git add -f .rex/workspace.json`), never `audit.jsonl`: it holds prompt lines and, with `audit_full_prompt`, whole prompts.

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

- rex never follows a symlinked `.rex/`, `workspace.json` or `audit.jsonl`, and refuses a `.rex/` that other users can write to or that another user owns (error: `unsafe .rex`, exit 6 on the CLI).
- `workspace.json` is read as data only: files over 64 KiB are rejected, the name is cut to 40 characters and stripped of control characters, a color that is not `#RRGGBB` is dropped, and an id that is not 8 hex digits is refused. Nothing in it changes how rex behaves.
- Every file rex writes under `.rex/` is readable by you only (`0600`, folders `0700`).
- A repository cloned from the internet can ship a `.rex/`. Opening it shows its workspace name and id, nothing more.
- `rex ws init` refuses `$HOME` and `/`.
- `rex ws init` writes outside `.rex/` once: two lines in `.git/info/exclude`, under the checks described above.
- `rex audit` and the daemon read at most the newest 8 MiB of an audit file; `rex audit` says when older records were left out.
- A session cleared before it ended (or still running) is recorded as `failed` with the note `deleted while running`. If its record cannot be written, the session is kept, an error is logged, and the next sweep or delete tries again.
