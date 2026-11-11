# R06 task plan (W1, W2, W4)

Status: W0 gate passed 2026-11-11 (decisions D1–D6 in [R06-workspaces.md](R06-workspaces.md) answered). This file breaks W1, W2 and W4 into build steps. W3 (workspace `config.lua`) waits for R05; W5 is deferred, W6 dropped.

Base: `integration/todo` at `c84d817` or later. Relevant code on that base:
- paths: `internal/runtime/paths/paths.go` (`Layout`, `DataDir`, `SessionsDir`, `ExpandHome`);
- spawn: `protocol.NewSession` (`internal/wire/protocol/intents.go:71`) and the `spawnSpec` path in `internal/daemon/server`;
- board filter: `rowFilter` (`internal/surface/tui/rowfilter.go:8`), with fields `tool`, `terms`, `hidden`, `ctx`;
- flags: `settings.FeatureOn` (FF0), section Features.

Round-1 U4 (window border) is not merged yet, so W2 shows the workspace color on the id chip and switches to U4's border when U4 lands.

## Global constraints
- Flags: `workspaces` (off) gates W1 and W2; `audit_trail` (on) and `audit_full_prompt` (off) gate W4. Off means inert: no `.rex/` reads or writes, no discovery, commands answer `off: enable in Settings → Features (rex config set <flag> true)` with exit 5.
- Every file rex writes under `.rex/` or `~/.local/share/rex/` is 0600, and every directory 0700. rex refuses to follow a symlinked `.rex/`, a symlinked `workspace.json`, `sessions.jsonl` or `audit.jsonl`, and a `.rex/` that is group- or world-writable or owned by another user (error `workspace.ErrUnsafe`, one notice).
- The daemon is authoritative for a session's workspace. Clients never send a workspace id on spawn; the daemon discovers it from the session CWD, so a client cannot tag a session into another workspace.
- Wire changes are additive: no `ProtocolVersion` bump.
- Tests never touch the real `$HOME`: `t.TempDir()` + `t.Setenv("HOME", …)` (and `USERPROFILE` on Windows).

## Review focus
1. A repo cloned from the internet ships a `.rex/`: discovery may read `workspace.json` (data only), but nothing in it can change rex behavior until W3 + `rex ws trust`. W1 test: a hostile `workspace.json` with oversized or odd fields is rejected or truncated.
2. Nested workspaces (`~/dev/.rex` and `~/dev/rex/.rex`): the nearest one wins. W1 test.
3. Workspace folder moved while sessions run: sessions keep `workspace_id`; the registry updates the root the next time `Find` sees the id at a new path. W1 test.
4. Two machines append to the same synced `audit.jsonl`: the reader tolerates a torn last line and duplicates (dedupe by `session_id` + `ended_at`). W4 test.
5. Read-only `.rex/` (or read-only filesystem): the board still works; writes fail with one notice per daemon run, not one per event. W2 and W4 test.

---

## W1 Identity

**Files**
- Create `internal/workspace/workspace.go`, `registry.go`, `exclude.go`, `swatch.go` + tests.
- Create `internal/surface/cli/workspace/ws.go` + tests; register `rex ws` in `cmd/rex/main.go` the way current kit commands are registered.
- Modify `internal/runtime/paths/paths.go`: `Layout.WorkspacesFile` = `<DataDir>/workspaces.json`; update the package doc and `docs/paths.md`.
- Modify `internal/catalog/settings/registry.go`: Features rows `workspaces`, `audit_trail`, `audit_full_prompt` (the audit rows ship in W4; adding them now is fine if their help says "W4").

**Interfaces (produced)**
```go
package workspace
type Workspace struct {
    ID        string    `json:"id"`         // 8 lowercase hex
    Name      string    `json:"name"`       // ≤ 40 runes, display only
    Color     string    `json:"color"`      // "#RRGGBB" from Swatches
    CreatedAt time.Time `json:"created_at"`
    Root      string    `json:"-"`          // folder that holds .rex/
}
var ErrUnsafe = errors.New("workspace: unsafe .rex")
func Find(cwd, home string) (*Workspace, error)                    // nil, nil when none
func Init(root, name string, reg *Registry) (*Workspace, error)
func Swatches() []string                                            // colorblind-safe list
type Registry struct{ /* path */ }
func OpenRegistry(path string) (*Registry, error)
func (r *Registry) Seen(w *Workspace) error                         // id → root, last_seen
func (r *Registry) Taken(id string) bool
func (r *Registry) List() ([]Entry, error)
func ExcludeInGit(root string) (bool, error)                        // adds "/.rex/" to .git/info/exclude once
```
- **IDs:** `crypto/rand`, retried up to 5 times while `reg.Taken`.
- **Color:** the first swatch not used by a registered workspace, else random.
- **Swatches** (Okabe–Ito, colorblind-safe): `#0072B2 #E69F00 #009E73 #CC79A7 #56B4E9 #D55E00 #F0E442`. If U4 merges first with its own list, use U4's list and delete this one.

**CLI**
- `rex ws init [path] [--name N]`: init, git exclude (D3), print id and color.
- `rex ws id [path]`, `rex ws ls` (from the registry), `rex ws rename <id|path> <name>`, `rex ws color <id|path> <#hex|swatch-index>`.
- All support `-o text|json|agent`.

**Steps** (test first, one commit each)
1. `Find` + `ErrUnsafe`. Tests:
   - finds the nearest `.rex/`; stops at `home` and the filesystem root;
   - symlinked `.rex/` and symlinked `workspace.json` refused;
   - group-writable `.rex/` refused (skip on Windows);
   - `workspace.json` > 64 KiB rejected; unknown fields ignored; `Name` truncated to 40 runes;
   - a bad id (not 8 hex) rejected.
2. `Init` + `Registry` + swatches. Tests:
   - files 0600, dir 0700;
   - id regenerated on a forced collision (`Taken` stub);
   - colors unique until the list runs out;
   - registry root updated when the same id is seen at a new path (Review focus 3).
3. `ExcludeInGit`. Tests: adds the line once (idempotent); no-op outside a repo; works with a `.git` file pointing to a gitdir (worktrees).
4. `rex ws` CLI + Features rows. Tests:
   - each subcommand in text and json;
   - flag off → exit 5 with the off message, and no `.rex/` created;
   - `init` on `$HOME` or `/` refused (same rule as `auto_trust_dirs`).
5. Docs: `docs/workspaces.md` (from [R06-workspaces-userdoc.md](R06-workspaces-userdoc.md), W1 parts), `docs/cli.md`, `docs/paths.md`, CHANGELOG.

## W2 Sessions tagged + workspace board

**Files**
- Modify `internal/wire/protocol/events.go`: `SessionSummary.WorkspaceID string \`json:"workspace_id,omitempty"\``.
- Modify `internal/daemon/state/session.go`, `persist.go` (Summary, fromSummary, applyPatch).
- Modify the spawn path in `internal/daemon/server`: discover the workspace from CWD when `workspaces` is on, set it, register it in the registry.
- Create `internal/workspace/index.go`: `AppendIndex(w *Workspace, line IndexLine) error` (`O_APPEND`, 0600, symlink refusal).
- TUI: `rowFilter` gains `workspace string`; `cmd/rex` gains `rex here` and `rex ws open [path]`, which start the TUI with that filter fixed; header shows `[7f3b89a2 rex]` top-left in the workspace color; new-session wizard CWD defaults to the workspace root.

**Index line**: `{"session_id","short_id","tool","model","name","cwd","started_at","ended_at","state","resume_id"}`, written at spawn and at the first terminal state.

**Steps**
1. Protocol + state field, persisted. Tests: JSON round trip with omitempty; restart keeps the field on crashed sessions.
2. Daemon tagging + index. Tests:
   - spawn in a nested folder tags the nearest workspace;
   - flag off → no discovery (stub `Find` counts calls = 0), no index file;
   - a read-only `.rex/` gives one log line per daemon run, and the spawn still succeeds (Review focus 5);
   - a client cannot set the id (the field is ignored on `NewSession`).
3. `rex here` / `rex ws open`. Tests:
   - no `.rex/` above `$PWD` → exit 5, "no .rex/ here — run `rex ws init`";
   - the board shows only that workspace's sessions;
   - an old daemon (summaries without the field) shows every session plus one notice "daemon doesn't tag workspaces — `rex daemon restart`".
4. Header id chip + wizard CWD default. Goldens (only the header row changes); wizard test.
5. Docs: `docs/workspaces.md` (W2 parts), `docs/tui.md`, `docs/protocol.md`, `docs/agents/data/schemas.md`, CHANGELOG.

## W4 Audit trail

**Files**
- Create `internal/features/audit/audit.go`, `reader.go` + tests.
- Daemon: a store subscriber that writes on the first terminal transition, plus a call in the shared teardown path (prune / reap / delete) **before** the session dir is removed, so a session is never deleted without its record. Idempotent per session via an `AuditWritten bool` kept in meta (not on the wire).
- Create `internal/surface/cli/inspect/audit.go`: `rex audit`.
- `paths.Layout.AuditFile` = `<DataDir>/audit.jsonl` for sessions outside any workspace.

**Record**: `{ts, workspace_id, session_id, short_id, name, tool, model, prompt_first_line, prompt?, state, started_at, ended_at, duration_s, resume_id, description}`.
- `prompt_first_line` comes from the session's initial prompt when the daemon has it, cut to 200 runes and run through `protocol.SanitizeDisplayText`.
- `prompt` (full text) only when `audit_full_prompt` is on (D5).

**Interfaces**
```go
package audit
type Record struct { /* fields above */ }
func Append(path string, r Record) error                    // O_APPEND, 0600, symlink refusal
type Filter struct{ Since time.Time; Tool, State, Workspace string }
func Read(paths []string, f Filter) ([]Record, []error)     // torn lines skipped; dedupe on session_id+ended_at
```

**CLI**: `rex audit [--ws <id|path> | --all] [--since 7d] [--tool T] [--state S] [-o text|json|md|agent]`. The default scope is the workspace around `$PWD` when `workspaces` is on, else global.

**Steps**
1. `Append` + `Read`. Tests: 0600; symlink refused; torn last line skipped; duplicates removed; filters.
2. Daemon writes. Tests:
   - the first terminal transition writes exactly once (a resume and a second end writes a second record with a new `ended_at`);
   - prune and reap write before the dir is gone (an order assertion through a fake remover);
   - the full prompt is absent unless the flag is on;
   - flag off → no file.
3. `rex audit`. Tests: scopes, filters, every `-o` format, empty-state text.
4. Docs: `docs/workspaces.md` (audit section), `docs/cli.md`, `docs/paths.md`, SECURITY.md (audit can hold prompt text when opted in), CHANGELOG.

**Deferred:** the TUI `:audit` page waits for R04's full-screen frame (after U1).

---

## Lanes when building
- W1 → W2 run in order (one builder).
- W4 can run in parallel with W1 (it only needs `workspace.Find` at its last step for the default scope; stub it until W1 merges).
- Merge order: W1, W4, W2.
