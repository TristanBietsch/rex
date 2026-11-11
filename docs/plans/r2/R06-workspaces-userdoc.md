# Draft: docs/workspaces.md (user doc for R06)

> Draft for the W0 gate. It becomes `docs/workspaces.md` piece by piece as W1, W2 and W4 ship; until then nothing here is implemented. Sections marked *(W3)* wait for workspace `config.lua`.

# Workspaces

A workspace is a folder that has a `.rex/` folder in it, like Cursor's `.cursor/`. rex groups the sessions you start anywhere inside that folder, gives the workspace a short id and a color, and keeps a history of its finished work there.

Workspaces are off by default. Turn them on in Settings → Features → `workspaces`, or with `rex config set workspaces true`.

## Start one

```sh
cd ~/dev/myproject
rex ws init --name myproject
```

This creates:

```
myproject/.rex/
├── workspace.json   # id, name, color
├── sessions.jsonl   # one line per session started here (written by the daemon)
└── audit.jsonl      # finished work (see "Audit trail")
```

`rex ws init` also adds `/.rex/` to the repo's `.git/info/exclude`, so git ignores it on your machine only. To share the workspace id and settings with your team, commit `workspace.json` (and `config.lua` *(W3)*) yourself.

## Open a workspace board

```sh
rex here               # the workspace around the current folder
rex ws open ~/dev/rex  # a workspace by path
rex                    # unchanged: the system-wide board with every session
```

A workspace board shows only sessions started inside that workspace. Its id and name sit in the top-left corner in the workspace color. New sessions start in the workspace folder by default.

## Ids and colors

Each workspace gets an 8-character id (for example `7f3b89a2`) when it is created, and a color from a colorblind-safe palette. Use the id anywhere a workspace is expected: `rex audit --ws 7f3b89a2`.

```sh
rex ws ls                       # known workspaces
rex ws rename 7f3b89a2 backend
rex ws color 7f3b89a2 '#D55E00'
```

## Moving between machines

rex has no sync of its own. `.rex/` lives in your project folder, so it moves with the folder: through git (if you commit it), Syncthing, iCloud Drive or a plain copy. On the other machine, `rex here` shows the workspace's history. Sessions from the first machine appear as ended; ones whose agent supports resume can be resumed if the agent's own history is there too.

Transcripts stay in rex's data folder (`~/.local/share/rex`) and do not travel with `.rex/`.

## Audit trail

When a session finishes, rex writes one line to the audit trail: who ran what, when, for how long, and how it ended. Sessions inside a workspace go to that workspace's `.rex/audit.jsonl`; others go to `~/.local/share/rex/audit.jsonl`.

```sh
rex audit                     # this workspace (or global, outside one)
rex audit --all --since 7d --state failed
rex audit -o json
```

By default a record keeps only the first line of the prompt. Turn on `audit_full_prompt` to keep the whole prompt; prompts can contain secrets.

## Workspace settings *(W3)*

A workspace can carry its own `.rex/config.lua` that overrides your global settings inside it. A cloned repo's settings are not applied until you trust the workspace with `rex ws trust`.

## Safety

- rex never follows a symlinked `.rex/`, and refuses one that other users can write to.
- Every file rex writes there is readable by you only.
- Opening a workspace someone else made can show you their history (whatever they committed), but cannot change how rex behaves until you trust it *(W3)*.
