# R06 Workspaces: per-folder `.rex/`, workspace ids and colors, system-wide rex, sync, audit trail

## Summary
- A folder can hold a `.rex/` directory (like Cursor's `.cursor/`). It holds that workspace's config, its session index and history, and its audit trail. Copy the folder to another machine and rex picks it up.
- `rex` with no arguments stays exactly as today: the **system-wide** board. A new entry point opens a **workspace** board scoped to one `.rex/`.
- Every board instance has a random 8-char **rex id** (top-left) and an **edge color** (U4's border). The system-wide board can list, create, delete, rename and recolor workspaces, and sync them between machines.
- This is an epic. It ships in phases; each phase is usable on its own. Phase W0 is a spec gate: the decisions below are answered and this file becomes the spec of record before code starts.

## Current behavior
- One daemon per state dir (`~/.local/share/rex`, `daemon.lock`). All sessions live in `sessions/<id>/{meta.json,transcript.log}`.
- `SessionSummary.CWD` already records where each session runs. `Fleet` is a user label, not a folder.
- No per-folder state, no audit log (sessions are deleted on prune/retire reap).
- Round-1 U4 adds a colored window border (random default, colorblind-safe swatches). T2 adds `ended_at`. T3 adds resume ids.

## Design

### Workspace = folder with `.rex/`
```
<root>/.rex/
├── workspace.json   # {"id":"7f3b89a2","name":"rex","color":"#0072B2","created_at":...}
├── config.lua       # optional, R05 evaluator, overrides global (phase W3)
├── sessions.jsonl   # index: one line per session ever started here (id, tool, model, name, started/ended, resume id, state)
└── audit.jsonl      # append-only completed-task log (phase W4)
```
- Discovery: nearest ancestor of the session CWD containing `.rex/workspace.json` (stop at `$HOME` and filesystem root).
- Transcripts stay in the daemon state dir by default (Decision D2), referenced by session id.
- `.rex/` git policy per Decision D3.

### Ids and colors
- `rex id` = 8 lowercase hex chars from `crypto/rand`, created once in `workspace.json`; the system-wide board's id lives in `~/.config/rex/system.json`.
- Collision check against known workspaces; regenerate on clash.
- Color: U4 swatch list (colorblind-safe), random unused swatch at creation; `:color` / `rex ws color` changes it. Board shows the id top-left and the color as U4's border on all four edges.

### Entry points (Decision D1)
- Recommended: `rex here` (or `rex ws open [path]`) opens the board filtered to the workspace that contains `$PWD`; `rex ws init` creates `.rex/`. Plain `rex` unchanged.

### Daemon model
- Still one daemon. Sessions gain `workspace_id` (additive wire field), set at spawn from CWD discovery. Workspace board = system board with a fixed workspace filter (reuses F1 `rowFilter`).
- Daemon writes `sessions.jsonl` / `audit.jsonl` lines on spawn and terminal transitions, with `O_APPEND`, 0600, and refuses symlinked `.rex/` files (SECURITY: a repo can ship a hostile `.rex/`).
- Trust: a cloned repo's `.rex/config.lua` is **not** evaluated until the user trusts the workspace (`rex ws trust`), mirroring auto-trust rules.

### Copy-to-another-machine and sync (Decision D4)
- W-portable (always): `.rex/` is self-describing; opening it on another machine shows history (index + audit) with sessions as "remote/ended", resumable where the tool's resume id works there.
- Sync options are a decision; recommended is "no live sync in rex; `.rex/` travels with the folder (git/Syncthing/iCloud), daemon re-reads on change".

### Audit trail (phase W4)
- `audit.jsonl` record: `{ts, workspace_id, session_id, short_id, name, tool, model, prompt_first_line, state, started_at, ended_at, duration_s, resume_id, description}`. Prompt text is opt-in (Decision D5); default stores first line only.
- Global audit at `~/.local/share/rex/audit.jsonl` for sessions outside any workspace.
- `rex audit [--ws id] [--since 7d] [--tool claude] [--state failed] [-o json]` (uses P4 output kit), and a `:audit` TUI page (R04 frame).

### CLI
`rex ws init|ls|open|rm|rename|color|trust|id`, `rex here`, `rex audit`. All take `-o` formats from P4.

## Phases and plan
- **W0 Spec gate.** Decisions D1–D6 answered; this file updated; a `docs/workspaces.md` user doc drafted. No code.
- **W1 Identity.** `internal/workspace` package: `Find(cwd) (*Workspace, error)`, `Init(root, name)`, id + color generation, `workspace.json` read/write with symlink + permission checks. Tests: discovery stops at `$HOME`; symlinked `.rex` refused; id regenerated on collision; Windows paths.
- **W2 Sessions tagged.** `workspace_id` on `NewSession`/`SessionSummary`/meta; daemon appends `sessions.jsonl`. TUI `rex here` filter, id top-left, U4 border color. Tests: spawn in nested folder tags the right workspace; workspace board hides others; old daemon (no field) shows everything with a notice.
- **W3 Workspace config.** R05 evaluator on `.rex/config.lua` behind `rex ws trust`. Tests: untrusted workspace config ignored with notice; precedence table extended.
- **W4 Audit trail.** Records on terminal transitions and on prune/reap (before deletion). `rex audit`, `:audit`. Tests: prune writes the record before removal; filter flags; 0600 perms; prompt text absent unless opted in.
- **W5 System-wide management.** System board lists workspaces (`:ws`), rename/recolor/delete (delete = remove `.rex/` index files only after confirm; never touches source files).
- **W6 Sync** per Decision D4.

## Review focus
- Repo cloned from the internet ships `.rex/config.lua` with `auto_trust_dirs = {"/"}`: must not apply before `rex ws trust`.
- Two machines append to the same synced `audit.jsonl`: lines may interleave; reader tolerates partial/duplicate lines (dedupe by session id + ts).
- Workspace folder moved or renamed while sessions run: sessions keep `workspace_id`; next discovery updates the root path.
- `.rex/` on a read-only filesystem: board works, writes fail with one notice.
- Nested workspaces (`~/dev/.rex` and `~/dev/rex/.rex`): nearest wins, documented.

## Decisions

| # | Question | Options (recommended first) |
|---|---|---|
| D1 | How to open a workspace board | (a) `rex here` / `rex ws open [path]`; plain `rex` stays system-wide · (b) plain `rex` auto-scopes when `$PWD` has `.rex/`, `rex --all` for system-wide · (c) TUI `:ws <name>` switch only, no new command |
| D2 | Where transcripts live | (a) daemon state dir, `.rex/` keeps index + audit only (small, no secrets in repo) · (b) inside `.rex/transcripts/` (fully portable, large, may hold secrets) · (c) setting per workspace |
| D3 | `.rex/` and git | (a) `rex ws init` adds `.rex/` to `.git/info/exclude` (local-only ignore); user can opt in to commit `workspace.json` + `config.lua` · (b) commit by default except transcripts · (c) do nothing |
| D4 | Sync between machines | (a) none in rex; `.rex/` travels with the folder (git/Syncthing/iCloud), daemon re-reads changes · (b) rex sync over SSH (`rex ws sync host:path`, rsync-style, index + audit only) · (c) daemon-to-daemon TCP with keys (live; large; overlaps R07 multiplayer) |
| D5 | Prompt text in audit | (a) first line only, full prompt opt-in setting · (b) full prompt always · (c) never |
| D6 | Scope of round 2 | (a) W0–W4 now (identity, tagging, config, audit); W5–W6 later · (b) all of W0–W6 · (c) W0–W2 only |
