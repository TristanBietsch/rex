# T3 Resume id column and resume action

**Summary**
- Add a tool-agnostic `Session.ResumeID` (wire and meta `resume_id`).
- For Claude it is deterministic: rex picks a UUIDv4 and spawns `claude --session-id <uuid>`. It stays correct after `/clear` or an in-app `/resume` because the existing injected SessionStart hook (session-scoped `--settings`, no global config writes) also reports `session_id`.
- Add a `Resume{session_id}` intent that respawns an exited, failed or crashed session in place (same row, id, slug and transcript) with `--resume <id>`. Expose it as `rex resume <sel>`, `:resume`, and (in T4) `uc`. The board shows a `resume` column (8-character prefix) in the detailed view.

**Current behavior**
- No resume id anywhere: `session.go:12-70`, `events.go:32-77`.
- Spawn argv is built in `server/spawn.go:52-62` (command + model args + effort + `trustArgsFor`), then hook args at :72-80, the env at :64-86, and the prompt delivery (`--` prompt) at :88-96 via `spawn_prompt.go:23`.
- Claude hooks are injected per session through `--settings` JSON (`adapter/claude_hooks.go:112-133`). The SessionStart hook already parses the hook JSON with `hookJSONField` (:96-100). Claude's hook input carries top-level `session_id`.
- Claude CLI 2.1.286 (checked locally):
  - `--session-id <uuid>` must be a valid UUID.
  - `-r, --resume [id]` reuses the original id unless `--fork-session` is passed.
- agy: `--conversation <id>`; it has no flag to preassign an id.
- Complete only kills (`server.go:627`, `supervisor.go:413`). A dead session cannot come back. After a daemon restart every live session is `crashed` and stranded (`persist.go:181`).
- Defer order in the spawn goroutine (`spawn.go:191-201`): when `hasStop(id)` is false, every other unregister call has already run (UnregisterStop runs last except `cancel` and `close(done)`). This makes `!hasStop` a safe "no live supervisor" test (`server.go:582`).

**Design: id capture (generic, agreed shape with A1)**
- Registry `Tool.Resume *ResumeSpec` (yaml `resume`), with `{id}` as the placeholder to match A1:
  ```yaml
  resume:
    args: ["--resume", "{id}"]            # required: argv to resume
    new_args: ["--session-id", "{id}"]    # optional: rex preassigns a UUIDv4 at spawn
    capture_regex: "..."                  # optional: group 1 = id, matched on termtext.Normalize(window) at child exit
  ```
  - claude: `args` + `new_args` (and the hook).
  - agy (A1 supplies): `capture_regex: "agy --conversation=([0-9a-fA-F-]{36})"`, `args: ["--conversation={id}"]`.
  - The placeholder may sit inside one argv element. Substitution never splits or adds elements.
- Loader (`registry/loader.go`):
  - `mergeOne`: an overlay `resume` block replaces the builtin one wholesale, like `TrustPrompt`.
  - `validate`: `args` is non-empty and contains `{id}`; `new_args`, when set, contains `{id}`; `capture_regex` compiles and has at least one group.
- Three id sources, later ones overwriting earlier ones. Every source passes `protocol.ValidResumeID` (new `internal/wire/protocol/resume.go`: canonical UUID regex only, as agreed with A1, which also rules out a leading `-`).
  1. **Spawn.** With `new_args`, `ids.NewSessionID()` produces a fresh UUID that is not the rex id: after `/clear` or a resume the two diverge anyway. It is set on `sess.ResumeID` before `AddUnique`, so `SessionAdded` already carries it.
  2. **Adapter.** New `adapter.ResumeIDReporter` interface (`ResumeID() string`) in `adapter.go`.
     - `ClaudeHooks` implements it. The SessionStart command adds `sid=$(hookJSONField("session_id") hookSafeID)` and emits `sid %s` in the same single printf (atomic line).
     - `apply` handles `sid` and ignores invalid ids.
     - The supervisor ticker (next to the `agents` poll, `supervisor.go:~455`) calls `Store.SetResumeID`. On a change it calls `persistMeta`, then `BroadcastPatch({"resume_id"})`.
  3. **capture_regex at exit.** In the `errc` exit branch (`supervisor.go:425-428`), after `refreshScreenText` and before the final `WriteMeta`: run the regex on `termtext.Normalize(window)`. If valid, `SetResumeID` and include `resume_id` in a patch broadcast after `WriteMeta`.
     - Also run it in the CompleteCh branch after `killAndReap`, in case the tool prints on SIGTERM.
     - The supervisor gets the compiled regex through `SupervisorConfig.ResumeCapture *regexp.Regexp`.
- **Why not scan transcripts under `~/.claude/projects`:** it reads outside rex's directories, depends on the cwd path encoding, and two sessions in the same cwd race. `--session-id` is deterministic, and the hook is already injected per session.

**Design: argv ordering in `spawn.go`**
- Extract a pure `buildArgv(tool, model, effort, cwd, resume resumeArgs) []string`. Order: command, model args, effort, trust args, then resume (`new_args` or `args` with the id substituted), then (unchanged) hook args, then prompt delivery appends `--` prompt last.
- A1 extracts `childEnvFor(tool, hookFile, id)` from lines 64-86. Disjoint lines, but the same function. Land whichever goes first as a pure refactor commit, and the other rebases.

**Design: Resume intent (daemon)**
- `protocol.IntentResume = "Resume"`, `Resume{SessionID}`. Handler `handleResume` in a new `server/resume.go`, registered in `defaultHandlers` (`handlers.go:21`). Semantics: "make this session live and not marked done."
  1. Unknown session → `unknown_session`.
  2. A live supervisor (`hasStop`) → `ClearRetire` plus commit `{retire_at:nil, retired_at:nil}` if it was marked, then Ack. Not marked → Ack, no-op.
  3. Dead (done, failed or crashed) → in-place respawn. Preconditions:
     - The tool and model are still in the registry (`spawn_failed` if not).
     - `tool.Resume != nil` and `ValidResumeID(sess.ResumeID)` (`bad_state` "session exited and <tool> cannot be resumed").
     - Not reaping (`bad_state`, message constant `protocol.MsgClearing`).
     - The cwd is still a directory (`spawn_failed`).
     - `srv.reserveStop(id)` (new, `server.go`): an atomic check-and-set of a placeholder in `stopFuncs` under `stopMu`, so two concurrent Resumes spawn once.
     - `TryAcquireSession` (`too_many_sessions`, with unreserve).
  4. `Store.Reopen(id)` (new, `store.go`), under `sess.mu`:
     - Refuses `reaping` or a non-terminal state.
     - Sets State=queued, LastEventAt=now; clears `EndedAt`, `ExitCode`, `RetireAt`, `RetiredAt`.
     - Keeps `StartedAt`, tokens, description and slug.
     - Does not broadcast. The server calls `commit(sess, {state:"queued", last_event_at, ended_at:nil, exit_code:nil, retire_at:nil, retired_at:nil})`.
  5. `state.ResetHookFile` (new, O_TRUNC) so the new adapter does not replay old hook lines.
  6. `startSupervisor(ctx, srv, sess, tool, argv, env, prompt="")`: the extracted tail of `handleNewSession` (`spawn.go:121-202`), shared by both paths. Trust args and the trust answer are recomputed from the current config.
- The transcript appends to the same file, so the attach replay shows both runs.
- Delete during the reserve window: StopSession calls the placeholder (no-op) and removes the session. The supervisor's `Transition(working)` then fails, and the existing `owned=false` defer kills the child. Covered by a test.

**Design: wire and back-compat**
- `SessionSummary.ResumeID string \`json:"resume_id,omitempty"\``. Patch key `resume_id`.
- `fromSummary` keeps the value only if `ValidResumeID`, so meta.json is not trusted for argv.
- Old clients ignore the field. Old daemons answer Resume with `bad_intent "intent not implemented"`; `friendlyDaemonError` already maps that to "run rex daemon restart".
- Client SDK: `(*Client).Resume(id, reqID)` in `wire/client/client.go`, next to `Unretire` (:652).

**Design: display**
- TUI `rowCols.resume`: width 8 (UUID prefix). Detailed view only, and only while some row has a resume id (the tokens pattern). It is the first column dropped at narrow widths (before the T2 span).
- Display keyed off F1's `boardGroup.Completed`: full style for Completed rows, dim on live rows.
- `applyPatch`: `resume_id`.
- `:rid [sel]` puts the full id in the status-line notice, so it can be selected with Option/Shift-drag.
- `:resume [sel]` sends Resume (`commandTarget`).
- CLI, new `internal/surface/cli/session/resume.go` `RunResume`:
  - `rex resume <sel>` sends Resume with `c.Do` and an Ack timeout. It prints "slug: resuming", "slug: unmarked" or "slug: already running", based on the snapshot state.
  - `rex resume -print <sel>` prints only the full id and sends no intent. Exit `ExitGeneric` "no resume id" if blank.
  - Wired in `cmd/rex/main.go`, `core/help.go` and the `meta/completion.go` verb lists.
- `rex ls -wide` (T2) adds `RESUME ID` (full) before NAME.

**Files to change**
- protocol:
  - `events.go`: `SessionSummary`.
  - `intents.go`: const plus `Resume` struct.
  - New `resume.go`: `ValidResumeID`, `MsgClearing`.
- registry: `types.go` (`ResumeSpec`, `Tool.Resume`), `loader.go` (`mergeOne`, `validate`), `builtin.yaml` (claude block only; A1 adds antigravity).
- adapter:
  - `adapter.go`: `ResumeIDReporter`.
  - `claude_hooks.go`: `claudeHookCommand` SessionStart case, `apply`, the `sid` field, `ResumeID()`.
- state:
  - `session.go`: field, `Summary`.
  - `store.go`: new `SetResumeID`, `Reopen`.
  - `persist.go`: `fromSummary`, new `ResetHookFile`.
- pty `supervisor.go`: `SupervisorConfig.ResumeCapture`, the ticker poll, the exit-branch capture (errc and CompleteCh).
- server:
  - `spawn.go`: split into `buildArgv`, `startSupervisor`, `handleNewSession`.
  - New `resume.go`: `handleResume`.
  - `handlers.go`: map entry.
  - `server.go`: new `reserveStop`, `unreserveStop`.
- client `client.go`: `Resume`.
- TUI:
  - `board.go`: `rowCols.resume`, `used`, `layoutRows` steps (the first entry), `renderRow`.
  - `board_times.go` or new `resume_view.go`: `resumeCell`, `resumeSessionCmd`, a `sessionResumer` interface.
  - `model.go`: `applyPatch`, a `resumer` injection field next to `retirer` (:76), and `friendlyDaemonError` for the `resume-` id prefix plus `MsgClearing`.
  - `command.go`: `executeCommand` cases `resume` and `rid`.
  - `help.go`: entries.
- CLI: `session/resume.go` (new), `cmd/rex/main.go`, `core/help.go`, `meta/completion.go`.

**Test plan (test-first)**
- Unit, protocol: `TestValidResumeID`. Accepts lowercase and uppercase UUIDs. Rejects `-x`, spaces, 37 characters, an embedded NUL and non-ASCII.
- Unit, registry:
  - `resume.args` without `{id}` fails validation.
  - An overlay replaces the block.
  - The builtin claude block is present and valid.
  - A bad `capture_regex` fails.
- Unit, adapter:
  - Run the generated SessionStart command through `sh -c` with a sample JSON on stdin (as the existing `claude_hooks_test.go` does). Assert one write with `needs_input` and `sid <uuid>`.
  - `apply("sid …")`: valid ids set it; invalid ids are ignored; the latest wins (`/clear`).
- Unit, server:
  - `TestBuildArgv`: claude new → `--session-id <uuid>` before `--`. Resume → `--resume <uuid>` and no `--session-id`. agy-style embedded `--conversation={id}` stays one element. Tools with no `resume` block produce byte-identical argv to today, checked for every builtin tool (regression).
- Unit, state:
  - `Reopen` refuses live and reaping sessions and clears the fields.
  - `SetResumeID` reports changes.
  - `fromSummary` drops an invalid `resume_id`.
  - `LoadAll` keeps a valid one on a crashed session.
- Integration (`testutil.StartDaemonWithTools`): fake tool `fake` with command `["bash","-c"]`, model args `['echo "ARGV: $0 $*"; sleep 0.3']`, and a `resume` block with `new_args` and `args`.
  - NewSession: `SessionAdded.resume_id` is a UUID and the transcript has `--session-id <id>`.
  - Done: then Resume gives the patch `state=queued, ended_at=null`, the transcript gains `--resume <id>`, and the session reaches done again.
  - Resume on a live session: Ack, one ARGV line.
  - Concurrent double Resume: exactly 2 ARGV lines in total.
  - A tool without `resume`: `bad_state`.
  - Unknown id: `unknown_session`.
  - A claimed retire (short TTL, sweep): `bad_state`.
  - Delete racing Resume: `hasStop` eventually false, no orphan.
  - Cap 1 with another live session: `too_many_sessions`.
  - A `capture_regex` tool whose script prints `id=<uuid>` on exit: the `resume_id` patch arrives and meta.json has it.
- TUI unit:
  - `applyPatch` `resume_id`.
  - The column appears only when some row has an id.
  - The column drops first at narrow width.
  - `:rid` notice.
  - `:resume` calls the fake resumer.
  - `friendlyDaemonError` mappings.
  - Layout widths test with ids.
- CLI: `RunResume` against a testutil daemon (pattern from `prune_daemon_test.go`); `-print`.
- Snapshot: detailed-board snapshot including the resume column.

**Docs, CHANGELOG, schema**
- `docs/protocol.md`:
  - Intents table: `Resume`.
  - Replies table.
  - Error codes: `bad_state` now also covers "not resumable" and "clearing".
  - SessionSummary fields: `resume_id`.
  - Compatibility note.
- `docs/agents/data/schemas.md`: ResumeID; Resume intent.
- `docs/registry.md`: a new `### resume` section (fields, placeholder, the three capture sources, UUID-only validation).
- `docs/cli.md`: `## rex resume`.
- `docs/slash.md`: `resume`, `rid`.
- `docs/tui.md`: ROW COLUMNS (resume), plus note that resume keeps the row, slug, start time and transcript.
- `docs/daemon.md`: crashed sessions can be resumed.
- `SECURITY.md`: in-scope sentence that resume ids reaching argv are validated.
- CHANGELOG: Added.
- `tools/coverage/baseline.txt`: raise floors if coverage rises.

**Conflict map**
- A1:
  - Owns the antigravity `resume` values in `builtin.yaml` and any agy adapter. T3 owns `ResumeSpec`, the validator, the capture plumbing and the intent.
  - `spawn.go`: A1's `childEnvFor` (env, lines 64-86) and T3's `buildArgv`/`startSupervisor` (52-62, 121-202) are separate regions of the same function. Agree who lands first. Recommend T3's refactor commit lands first as a pure no-behavior-change refactor.
  - `spawn_prompt.go` is untouched by T3.
- F1: no overlap in `board.go` columns. T3 reads `boardGroup.Completed`.
- T5: adds `ResumeID` to its search haystack (one line, after T3 step 1).
- T4: depends on the Resume intent and client method.
- P1–P3: `cmd/rex/main.go` switch, `core/help.go`, `completion.go` verb lists, and `ls -wide`. These are append-only edits, expected to merge trivially.
- U1/U2: `help.go` entries.
- S4/S5: none.

**Security notes**
- A resume id becomes argv for a spawned process. It is validated as a canonical UUID at all four points (hook ingest, regex capture, meta load, spawn). Substitution is within one argv element, and exec has no shell, so there is no flag or shell injection.
- The hook value is first stripped by `hookSafeID` in the shell.
- The Resume intent grants nothing beyond NewSession (anyone on the socket can already spawn). It re-runs only the registry command with the session's stored cwd and model, after re-checking the cwd and recomputing auto-trust from the current config.
- No writes to `~/.claude` or other global config. `--settings` stays session-scoped. Nothing reads `~/.claude/projects`.
- Display goes through the `cell()` sanitizer.

**Open questions**
1. `rex new -resume <uuid>`, to start a new rex session from a raw id (for example after a prune)? Default: not now; follow-up.
2. Copy the full id to the clipboard with OSC 52 (`y` key)? Default: no. `:rid` and `rex resume -print` are enough.
3. Show the resume column in the simple view's Done rows? Default: no, detailed view only.
4. If Claude never saved the conversation (killed before the first prompt), `--resume` fails and the row lands in failed with Claude's message. Accept that? Default: accept and document; no auto-fallback.

**Ordered steps (each one commit, `make check` green)**
1. `refactor(server): split spawn into buildArgv and startSupervisor`: no behavior change; argv regression test for every builtin tool.
2. `feat(protocol,state): resume_id field and ValidResumeID`: field, persist with validation, schemas and protocol docs.
3. `feat(registry): resume block (args, new_args, capture_regex)`: types, loader, claude builtin, registry.md.
4. `feat(daemon): preassign Claude session ids and capture resume ids`: buildArgv `new_args`, the hook `sid`, ResumeIDReporter, the supervisor poll and exit capture, integration tests.
5. `feat(daemon): Resume intent respawns an exited session in place`: Reopen, reserveStop, ResetHookFile, handleResume, client.Resume, integration tests, protocol.md, daemon.md.
6. `feat(tui): resume column, :resume and :rid`: board, model, command, help, tests, tui.md, slash.md.
7. `feat(cli): rex resume [-print]`: plus help, completion, cli.md, CHANGELOG.

**Size:** L
