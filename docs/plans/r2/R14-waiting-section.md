# R14 Waiting section (agent is busy, not waiting on you)

## Summary
- User ask: "add a section to rex: waiting. it has its own unicode icon, and is below NEEDS YOU. this is for when an agent is waiting for something like a shell to finish or does not need user input."
- Today a Claude session that runs a long shell command, a subagent or a web fetch shows as Working with the same spinner as one that is thinking. The user cannot tell "the agent is blocked on a tool" from "the agent is generating".
- New board section **Waiting** between Needs you and Working, with its own marker `◷`. Source: Claude Code hooks (the only agent with hooks today).
- No new protocol state. Two additive summary fields; an old TUI keeps showing the session in Working.

## Current behavior
- `boardGroups` (`internal/surface/tui/board.go:47`) is the table of three sections: needs, working, done. `Match func(SessionSummary) bool` has no clock. The index constants `groupNeeds/groupWorking/groupDone` (`board.go:86`) are used by `header.go:25`, `keymap.go:21,43`, `update.go:384` (keys `1`, `2`, `3`) and tests.
- Invariant (comment at `board.go:41`): every state lands in exactly one group, or the session is invisible. `columnCounts` (`board.go:68`) and `partitionSessions` (`board.go:175`) take the first match; failed/crashed are counted apart as "failed".
- `resolveSection` (`board.go:129`) maps an id, a unique prefix, the title, or a 1-based number to an index. Caller: `filter.go:52` (`:hide`/`:show`/`:only`). `z1`-`z9` (`filter.go:150`) index `boardGroups[n-1]`.
- Header counts: `renderHeaderCounts` (`header.go:95`), three styles (`verbose`, `glyphs`, `numbers`). Screensaver: `saverCounts` (`screensaver.go:234`) walks `boardGroups`.
- Marker: `stateMarkerCellFor` (`board.go:687`), spinner frames from `spinnerFramesFor`; `reduce_motion` gives a static `*`. Animation: `SpinnerTickMsg` every 100 ms (`events.go:62`) while `hasWorkingSessions` (`board.go:254`).
- Hooks: `claudeHookStates` (`internal/daemon/adapter/claude_hooks.go:30`) maps PreToolUse/PostToolUse to `working` with matcher `*`. `claudeHookCommand` (`:97`) builds one printf per event, atomic with the state word, extracting JSON fields with `hookJSONField` (tr + sed, no jq). `PostToolUse` already branches on `agent_id` to tell a subagent's tool call. `ClaudeHooks.apply` (`:258`) is the line parser. `hookStaleWorking` (`:25`) turns a silent `working` into `needs_input` after 15 s.
- Daemon: `Store.Transition` (`state/store.go:233`) maintains `working_since` and patches; `Store.SetSubagents` (`:462`) is the model for an adapter-fed side channel; the supervisor asserts `adapter.SubagentCounter` once (`pty/supervisor.go:397`) and polls it on the tick. `Session.Summary` (`state/session.go:85`) and `fromSummary` (`state/persist.go:402`) convert. The TUI merges patches in `applyPatch` (`tui/model.go:299`).
- CLI: `rex ls` `lsView` (`surface/cli/session/ls.go:95`), `stateLabel` (`:64`); `rex status` `countSessions` (`surface/cli/inspect/status.go:63`), exit 1 only for `needs_input`.

## Design

### 1. Data model: additive, no new state
`State` stays `working`. Two new `SessionSummary` fields (`wire/protocol/events.go:32`):

| Field | JSON | Meaning |
|---|---|---|
| `WaitingOn string` | `waiting_on,omitempty` | `shell`, `subagent` or `web`; empty = not waiting on a tool |
| `WaitingSince time.Time` | `waiting_since,omitzero` | when the main thread started that tool call |

- Patch keys `waiting_on` / `waiting_since`; `null` (or `""` for `waiting_on`) clears. Both fields persist in `meta.json` like the rest of the summary.
- A session that is not `working` has neither field. `Store.Transition` clears them (and patches `null`) on any move out of `working`; `fromSummary` restores them only for a `working` summary (a daemon restart maps live states to `crashed`, so they are dropped).
- `ProtocolVersion` is not bumped. An old TUI ignores both fields and shows the session in Working (tested: the old-client view).
- Constants `protocol.WaitShell/WaitSubagent/WaitWeb` and `protocol.ValidWaitKind`. The daemon store rejects an unknown kind (error, no patch); the TUI patch merge ignores an unknown kind (treats it as not waiting), because a newer daemon may add kinds.

### 2. Source: Claude Code hooks
Extra line in the hook file, same printf as the state word:

| Event | Main thread (no `agent_id` in the hook JSON) | Subagent tool call |
|---|---|---|
| PreToolUse | `working` + `wait+ <kind>` when `tool_name` is `Bash` (shell), `Task` or `Agent` (subagent), `WebFetch` or `WebSearch` (web); else `working` only | `working` only (never marks the main session) |
| PostToolUse | `working` + `wait-` | `working` + `agent~ <id>` as today (no `wait-`) |

- Extraction: `hookJSONField("tool_name")` and the existing `case "$in" in *'"agent_id"'*` pre-check. No jq, no new dependency. The kind is mapped in shell with a `case`, so only the three fixed words can be written.
- `PostToolUseFailure` is not in the hook table and is not added: an unverified event name could make an older Claude reject `--settings`. A failed tool call therefore leaves the wait set until the next main-thread tool call, Stop, Notification or prompt. See "Follow-ups".
- `ClaudeHooks` keeps `waitOn` / `waitSince`. `wait+` sets (since = hook-file read time, `a.now()`), `wait-` clears. They are also cleared by: a `needs_input` word (Stop, Notification, SessionStart), the stale-working path in `Detect`, a truncated hook file, and process exit. The adapter reports through a new `adapter.WaitReporter` interface (`Waiting() (kind string, since time.Time)`), which returns empty unless the adapter's last state is `working`.
- The supervisor asserts `WaitReporter` once (like `SubagentCounter`), calls `Store.SetWaiting(id, kind, since)` on each tick (a no-op when unchanged; empty unless the session is `working`), and clears on every exit path (sibling of `clearAgents`).
- Heuristic adapters and OSC 7501 are out of scope.

### 3. Anti-flicker threshold in the TUI
- `const waitingAfter = 3 * time.Second` (tui). A working session is *waiting* when `WaitingOn != ""`, `WaitingSince` is set and `now - WaitingSince >= waitingAfter`. Below that it is Working, so quick tool calls never move rows.
- One pure predicate `isWaiting(s, now)` (in `board.go`), which calls `protocol.SessionSummary.WaitingFor`. Grouping takes a `groupCtx{now time.Time, waiting bool}` carried by `rowFilter` (`rowFilterOf(m)` fills it from `m.now()` and the flag); `boardGroup.Match` becomes `func(SessionSummary, groupCtx) bool`. Grouping stays pure: tests pass a fixed `now`.
- The threshold lives in the protocol package as `protocol.WaitingAfter` (3 s) next to `SessionSummary.WaitingFor(now)`, so `rex ls` and the board share one definition (no separate TUI const).
- `isMarkedDone` sessions stay in Done; `needs_input` is never Waiting (the predicate requires `State == working`); `queued` stays Working.

### 4. Section order and group
Needs you, **Waiting**, Working, Done:

```go
{ID: "waiting", Title: "Waiting", Count: "waiting", Empty: "nothing waiting on a tool", Alert: false, Match: ...}
```

- `groupWaiting = 1`, `groupWorking = 2`, `groupDone = 3`. Working's Match excludes sessions the Waiting group claims, so each session lands in exactly one group (tested over every state x WaitingOn x age x marked-done).
- `jumpToSection`, `visibleRows`, `orderedSessions` (j/k order), mouse hit testing and scrolling all read `boardGroups`, so they follow with no extra code.

### 5. Icon and animation
- Marker `◷` (U+25F7). While waiting the row marker cycles `◴ ◷ ◶ ◵` one step per second, from `m.now().Unix()` (redraws come from the existing 100 ms spinner tick, which already runs while any session is working). Static `◷` when `reduce_motion` is on and in the static paths (`rex render`, snapshots: they pin or omit the clock; a test pins this).
- Colour: the working colour, no bold alert (it reads as "busy, not for you").
- Attach header chip: `◷ waiting (shell)` while it applies.
- Width: all four glyphs are width 1 in `ansi.StringWidth` and `lipgloss.Width` (test in `board_waiting_test.go`); the marker column is `colMarker = 1`.
- Detailed view (`v`): the description cell of a waiting row is prefixed `shell 42s · ` (reason and age since `WaitingSince`).

### 6. Counts and text
- Header verbose: `N need you · N waiting · N working · N done`; the waiting part is left out when 0. `glyphs`: `◆N  ◷N  ⟳N  ●N` (waiting left out when 0). `numbers` keeps three numbers (waiting folded into working: a bare `a · b · c` has no label to carry a fourth).
- Screensaver `saverCounts` walks `boardGroups`, so it includes waiting (hidden at 0, matching the header).
- Waiting sessions: no sound, no notification (neither exists for working; Waiting is not an Alert group and no new event is added).
- `rex ls`: text/md STATE shows `waiting (shell)`; the script-stable `txt` column and the `agent` `state=` key keep `working` (`waiting_on` is a separate agent column); JSON carries the fields.
- `rex status`: unchanged counts and exit codes; a waiting session counts under working. Pinned by a test.

### 7. Keys follow screen order
- `1` Needs you, `2` Waiting, `3` Working, `4` Done (fold/unfold). **This changes what `2` and `3` did** (CHANGELOG "Changed").
- `resolveSection(m, ref)` accepts `waiting` (id, prefix `wa`, title, number). `w` is now ambiguous (waiting, working): the existing prefix rule returns an error naming the ids; `wo` and `wa` are unique. The old test `"w" -> working` changes deliberately.
- `z1`-`z9` use the same numbering (`z2` Waiting, `z4` Done); `zh` still means the completed section. Section ids `needs`/`working`/`done` are unchanged, so `board_hidden_sections` keeps working; `waiting` is hideable too.
- Help overlay, key legend and `docs/tui.md` change the `1`/`2`/`3` rows. The Done title hint (`3 hide` / `3 show`) becomes `4`.

## Feature flag
- `waiting_section` (bool, default **on**), Settings → Features (the section exists since FF0: `SectionFeatures`, `settings.FeatureOn`). The registry contiguity test already covers it. If a build without the Features section is ever merged under this, put the row in Behavior after the board rows and move it when FF0 lands.
- Off: the TUI classifies waiting sessions as Working; the Waiting section, its count and its key disappear; keys `1`/`2`/`3`/`z1`-`z3` revert to needs/working/done (`4`/`z4` do nothing); `resolveSection` rejects `waiting`; the marker is the normal spinner; a hidden-section id naming `waiting` is ignored. `rex ls` shows plain `working`.
- **Deviation from D15's "off is inert":** the daemon still records `waiting_on`/`waiting_since` and the hook still writes `wait+`/`wait-`. It is a few bytes in a file the hook already writes and one string in a patch, not a running feature (no goroutine, subprocess, network or extra file). A daemon-side gate would need a live settings read per tick and would lose the current wait on a flag flip. The off-state tests are on the TUI/CLI side.
- Mechanism: grouping context `waiting=false` makes the Waiting group match nothing and Working keep everything; `showsSection` hides the group, so the slot, count and chip code need no second branch. A helper `activeGroups(m)` (indices shown under the flag) feeds the number keys, the `z` chords, hint text and `resolveSection(m, ref)`. A hidden-section list that names every active section is ignored (a stale list never blanks the board when the flag goes off).
- `rex ls` reads the flag from `config.yaml` (`settings.LoadDefault`), so off shows plain `working` there too.

## Plan
Each step = one commit with its tests, `make check` green. (As built: the flag row, number keys and `resolveSection` landed with the grouping in R14-3, because grouping needs the flag and the index shift breaks the key tests in the same commit.)

1. **R14-1 Wire and store fields.** `protocol.SessionSummary.WaitingOn/WaitingSince`, kinds, `WaitingFor(now)`, `WaitingAfter`; `Session` fields, `Summary`; `Store.SetWaiting`; `Transition` clears; TUI `applyPatch` keys. Tests: JSON round trip with omitempty/omitzero; patch null/empty clears; meta records the fields but `LoadAll` does not restore them; `Transition` out of `working` clears and patches null; unknown kind rejected.
2. **R14-2 Hook lines and adapter.** `claudeHookCommand` PreToolUse/PostToolUse; `ClaudeHooks` wait state and `WaitReporter`; supervisor wiring; a terminal `Transition` always patches the clear. Tests with synthetic hook JSON run through `sh` (main-thread Bash/Task/Agent/WebFetch/WebSearch write `wait+`; Read writes nothing; a subagent PreToolUse with `agent_id` does not); `wait-`, Stop, Notification, SessionStart, truncation and stale-working clear; the supervisor sets and clears through the store.
3. **R14-3 Board group, counts, keys, flag.** `groupCtx`, `isWaiting`, the `waiting` group, index constants, `partitionSessions`/`columnCounts`/`filterByGroup` take the ctx; header and screensaver counts; number keys, `z`, `resolveSection(m, ref)`, `activeGroups`; `waiting_section` setting; Done hint `4`. Tests: grouping table (state x WaitingOn x age around 3 s x marked-done x flag: exactly one group, threshold, needs_input never waiting); old-client view stays in Working; header and saver strings; keys 1-4; flag off removes the group/count and reverts keys; existing key/z/filter tests updated for the new numbering.
4. **R14-4 Icon, animation, detail.** Marker, 1 s cycle (10 spinner ticks), still at tick 0 and under reduce_motion, attach chip, detailed prefix. Tests: frame per second; static under reduce_motion; width 1 for all four glyphs; detailed text `shell 42s`.
5. **R14-5 CLI.** `rex ls` label and header count, status pinned. Tests: ls text/md/txt/agent/json, flag off, status counts.
6. **R14-6 Docs.** `docs/tui.md`, `docs/protocol.md`, `docs/agents/data/schemas.md`, `docs/agents/modules/adapter.md`, `docs/cli.md`, `docs/settings.md`, CHANGELOG (Added + Changed).

## Review focus
- Subagent isolation: a subagent's PreToolUse/PostToolUse must never set or clear the main session's wait. A main-thread `Task` keeps the session Waiting (`subagent`) while the subagent's own tools run (they emit `working` + `agent~`, no `wait-`).
- Parallel main-thread tool calls: the wait is one slot. A `wait-` from a finished Read clears a still-running Bash (shows Working early); a second `wait+` overwrites the first kind. Acceptable, it errs toward Working. A depth counter was rejected: one lost hook would stick a session in Waiting for good.
- A failed tool call (no PostToolUse) leaves the wait until the next main-thread event.
- The 15 s `hookStaleWorking` path: a long silent shell must not flip to `needs_input` (Claude animates its spinner during a Bash call; confirm with a live run, not covered by tests).
- The invariant: every session in exactly one group, including with the flag off and with sections hidden.
- Number keys: `2`/`3` changed meaning; `z`, `:hide`, help text and tests all use one numbering source.
- Mixed versions: new TUI + old daemon never gets the fields (Working); old TUI + new daemon ignores them.
- Merge conflicts: S4 `wait_reason` edits the same hook command builders and adapter `apply`; T1 hidden sections edit `filter.go` and `rowfilter.go`; S3 touches `Session`/`Summary`.

## Follow-ups (not in this spec)
- R02 OSC 7501 `working` reports can feed `WaitingOn` for other agents.
- `PostToolUseFailure` hook once the minimum Claude version is known.
- Heuristic adapters (`busy_regex`) could report `shell` from a status line.
- A "waiting for N min" badge if a wait runs very long.

## Decisions (given 2026-11-11, not re-asked)

| # | Question | Answer |
|---|---|---|
| D1 | Data model | Additive fields `waiting_on` / `waiting_since`; state stays `working`; no protocol bump. |
| D2 | Source | Claude hooks only: `wait+ <kind>` / `wait-` on main-thread PreToolUse / PostToolUse. |
| D3 | Flicker | 3 s threshold in the TUI (`waitingAfter`), not in the daemon. |
| D4 | Order | Needs you, Waiting, Working, Done. |
| D5 | Icon | `◷`, cycling `◴ ◷ ◶ ◵` at 1/s; static with `reduce_motion`. |
| D6 | Counts | Header and screensaver show waiting; `rex ls` `waiting (shell)`; status exit code unchanged; no sound. |
| D7 | Keys | `1` `2` `3` `4` follow screen order; CHANGELOG "Changed". |
| D8 | Flag | `waiting_section`, default on; off groups into Working (daemon still records). |
