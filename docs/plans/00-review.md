# Review of the 19 rex plans (F1, T1–T5, S1–S5, U1–U3, P1–P3, A1, PL2)

I spot-checked about 70 file:line and behaviour claims. Most hold. `go build ./...` passes. No files were edited.

I found 15 contradictions between plans, 1 more bug in plain sight (T1 adds a second "Behavior" section to the settings page) and a few wrong claims. All have fixes below. Five decisions need you before building; the rest can take the planners' defaults.

## Contradictions (each with a fix)

1. **Four plans rewrite `handleNewSession` (`server/spawn.go:23-203`).** T3 adds `buildArgv`/`startSupervisor`, A1 adds `childEnvFor`, P1 adds skill args, P2 adds `trustDecision`, and U3 changes the unknown-tool error at :35-38.
   - **Fix:** land one combined refactor commit first (T3-1 plus A1-1) that defines a single spawn spec covering all of them:
     ```go
     type spawnSpec struct{ Tool registry.Tool; Model registry.Model; Effort, CWD string
       Trust trustDecision /*P2; explicit=false until P2*/; Resume *resumeArgs /*T3*/; PluginDir string /*P1*/
       HookFile, SessionID string }
     func buildArgv(spawnSpec) []string   // command, model, effort, trust, resume, skills, hooks; then initialPromptDelivery adds `--` prompt
     func childEnvFor(spawnSpec) []string
     ```
   - `buildArgv` cannot be pure if it is given only `cwd`, because `trustArgsFor` reads config through `srv`. Pass the already-computed trust decision instead.
   - **Gap:** Resume rebuilds argv from the persisted Session. P1 persists `Skills`, but P2 does **not** persist `TrustFolder`. A resumed agy session that was trusted only for that session would show agy's trust dialog again. Add `Session.TrustFolder` to meta, and have T3's `handleResume` re-add `--plugin-dir` when `Skills` is set.

2. **What `:resume` means: T3 vs T4.**
   - T3: `:resume [sel]` sends the Resume intent directly.
   - T4: `case "uc","resume"` → `undoComplete`.
   - **Fix:** T4 wins. `:resume` and `:uc` both call `undoComplete`, which calls T3's resumer for terminal rows. T3 step 6 adds only `:rid` and the resumer wiring. `friendlyDaemonError`'s `resume-` prefix is owned by T3-6.

3. **The registry `resume` block.** T3 and A1 agree on the shape `{args, new_args?, capture_regex?}` and the `{id}` placeholder. **No `{resume_id}` appears anywhere.**
   - Small mismatch: A1 validates ids with `^[0-9a-f-]{36}$` (lowercase only); T3's `ValidResumeID` is a canonical UUID check that accepts either case.
   - **Fix:** use T3's validator everywhere and lowercase ids on ingest.
   - Note that `{id}` is replaced inside one argv element, while today's effort `{value}` is split on whitespace (`spawn.go:57-59`). Document the difference in `registry.md`.

4. **Row accounting (`boardBottomRows` / `headerHeight`).** These plans are consistent once two wrong assumptions are removed:
   - S3's conflict map says T5's search bar and S5's banner add board rows. They don't: T5 reuses the prompt line, and S5 is attach-only.
   - T1's conflict map says S5 edits `renderHeader`/`headerHeight`. It doesn't.
   - **Rule:** `headerHeight = 1 + chip row (T1's wider show predicate) + overview line (S4, placed after the chips) + backend banner`, and `boardBottomRows(m) = 3 + previewRows(m)` (S3).
   - `headerHitAt` (`board_mouse.go:156-159`) keeps its "chip row is row 1" assumption only if S4 stays after the chips. S4 does that.

5. **Settings insertion order.** The settings page draws a new section header whenever `Section` changes while walking `settings.Registry` in order (`settings.go:553-559`).
   - T1 "appends" `board_hidden_sections` (Behavior), which would print a **second "Behavior" header** after Advanced. S3's map repeats the "append" idea.
   - **Fix:** put it in the Behavior block after `screensaver_keep_awake` (`registry.go:~99-104`). The final Summary section order is `summary_enabled, summary_model, summary_detail (S3), desc_animation, overview_enabled (S4), attach_banner (S5)`.
   - Also add a registry test that each section's rows are contiguous.

6. **Daemon settings watcher (S3 vs S4 vs U2).**
   - S3 calls it `WatchSummaryDetail`; S4 and U2 call it `WatchSummarySettings` (a name mismatch). S4 generalizes it to `WatchSettings(ctx, path, every, apply ...func(*settings.Store))`. U2 replaces it with `SettingsHub`.
   - **Fix:** S3-4 builds S4's generic `boot.WatchSettings` directly. S4-5 only adds an applier. U2-8 wraps the same poller as `SettingsHub` and adds SIGHUP and reload. Build one watcher, not three.

7. **`Clean` vs `cleanResponse`, and ownership of `detail.go`.**
   - S2-2's `Summarize` returns `Clean(resp, MaxRunes, tool)`, but `Clean` only exists after S3-1. S3-2 adds `ParseDetail` to S2's `detail.go`.
   - **Fix:** order S3-1 → S2-1 → S2-2 → S2-3 → S3-2. `cleanResponse(s)` stays a shim for `Clean(s, 60, "")` until S3-7. S2 owns the `Detail` type and the caps table; S3 adds only `ParseDetail`/`SetDetail`.

8. **`filterTools` vs `filterToolIDs`.**
   - F1's test loops over `filterTools`. T1 validates `:filter <tool>` against `filterTools` plus any tool on the board. U3 replaces the var (`keymap.go:214`, which still lists codex and ollama) with `filterToolIDs(m)`.
   - **Fix:** land U3-4 before T1-1. T1 calls `filterToolIDs(m)`, and F1's test uses a fixed literal list.

9. **New `NewSession` fields:** P1 `Name`, `Skills`; P2 `TrustFolder`. They don't overlap. Order P1-3 before P2-2, both after the step-1 refactor. P2 must also persist `TrustFolder` (see #1).

10. **New `SessionSummary` fields:** `ended_at` (T2), `resume_id` (T3), `description_at` (S1), `wait_reason` (S4), `skills` (P1). These are additive, but each also touches `Summary()`, `fromSummary` (`persist.go:372`) and `applyPatch`.
    - **Fix:** land in DAG order and rebase each time.
    - S4's "not restored on load" is half-true: meta.json is `MarshalIndent(Summary())` (`persist.go:107`), so `wait_reason` is still *written* to disk. That's harmless.

11. **Claude hook edits: T3's `sid` vs S4's `wait_reason`.** Both edit `claudeHookCommand` (`claude_hooks.go:94`), `apply` (:238) and the supervisor tick.
    - Not contradictory, but they collide. T3 must print `sid` in **both** SessionStart branches (the compact branch at :99 too). S4 must not treat a `sid` line as clearing `wait_reason`.
    - Land T3-4 before S4-3. The adapter coverage floor is 98% (`baseline.txt:9`).

12. **Supervisor poll and exit paths.**
    - T3 `ResumeIDReporter`, S4 `WaitReasoner` and the existing `SubagentCounter` all go in the ticker. Type-assert each once before the loop, like `agents` at `supervisor.go:389`. A1's `OutputObserver` goes in the reader goroutine after :345.
    - Exit order for both the errc and CompleteCh branches: `refreshScreenText` → capture_regex (T3/A1) → clear side-channels → `SetExit`/`WriteMeta` → `Transition` → `resume_id` patch.
    - The CompleteCh branch (:413-424) has no `refreshScreenText` today (see factual errors).

13. **U1's `commands` table vs command additions in T1, T3, T4, T5, S4, P2 and U2.** U1 says it lands last and folds everything in.
    - **Fix:** land U1-7 (the table refactor, which needs no goldens) right after F1. Each feature then adds a table row with its help text, which ends the help.go drift that every plan mentions.
    - Keep U1-1 (goldens) until the final UI wave. Goldens on 15 parallel branches would conflict on every merge.

14. **U2's sparse Save vs T1's `:bg`.**
    - U2 says T1's `:bg` must use the dirty-key Save. **Wrong:** `:bg` writes `tui-state.json` through `SaveTUIState` (`tui/persist.go:23`), not the settings store, so T1 needs no change.
    - The real writers to `config.yaml` are P2 (`ctrl+a`, `rex trust`), P3-H4 (setup), U1-11 (inline edit) and `configSet`. Pull **U2-2** (read-modify-write sparse Save) forward into wave 1, independent of the Lua decision. P3-H4 then shrinks to "Set only my keys".

15. **Smaller collisions:**
    - **Unknown tool, error code:** U3-5 makes an unknown tool on NewSession `bad_intent`, but T3 maps a missing tool on Resume to `spawn_failed`. Use one helper and one message with `bad_intent`, so the CLI exits 5 in both cases.
    - **`color_scheme`:** U1-5 extends its enum options; U2-3 turns it into the `theme` string with dynamic options. Sequential, but U1's CLI `core.Styles()` must read `theme` once U2 lands.
    - **`auto_trust_dirs` inline edit:** U1-11 validates it itself. Route it through `Store.Set`, which runs `Setting.Normalize` (P3-H1), so there is one validator.
    - **doctor `runAllChecks` append order:** A1 `checkAgyVersion` → U3 `checkRegistryWarnings` → P2 `checkAutoTrustDirs`.
    - **Focus enum:** T5 `FocusSearch` and S4 `FocusOverview` are both appended; this is a trivial rebase.
    - **T4's worry about F1's chord API:** F1's `chordHandler(m, second)` is keyed by the first key, so `u`→`c` already works. Drop T4's proposed `second` field.

## Global build order

Up to 3 builders, each in a separate local worktree, merging into a local integration branch (no push). Lane A = TUI, B = daemon/registry/protocol, C = CLI/settings/summarizer. Within a lane, steps run in the order listed.

| Wave | Lane A | Lane B | Lane C |
|---|---|---|---|
| **1** foundations, no decisions | F1-1, F1-2, F1-3 → T4-1 (`:undone` fix) → U1-2 (style init order, theme race, NO_COLOR cursor) | **T3-1 combined with A1-1** (spawn spec refactor, see #1) → U3-1 (`validateTool` + retired-tool guard) → T2-1 (`ended_at`) | P3-1 (H11) → P3-2 (`ParseArgs`, `-h`) → P3-3 (H1 `Normalize`) → **U2-2** (sparse Save) |
| **2** | U3-4 (`filterToolIDs`) → U1-7 (command table) → T1-1…T1-8 → T2-2 | U3-2, U3-3, U3-5, U3-6 → T3-2, T3-3, T3-4 | S1-1 → A1-3 (synthetic agy fixtures) → S1-2, S1-3, S1-4 → S3-1 → S2-1, S2-2 |
| **3** | T5-1…T5-5 → T4-2 | T3-5, T3-6, T3-7 → T2-3 → T4-3 → A1-2 | S1-5, S1-6 → S2-3, S2-4 → S3-2…S3-7 (S3-4 as generic `WatchSettings`) → S1-7, S2-5 |
| **4** | P1-1…P1-8 | S4-1…S4-9 | P3-4…P3-11 |
| **5** | P2-1…P2-6 | S5-1…S5-6 | A1-4…A1-7 (only with capture consent) |
| **6** | U1-1, U1-3…U1-6, U1-8…U1-12 (one builder; touches every TUI file) | (A1-8…A1-10 only if hooks are approved) | — |
| **7** | U2-1, U2-3…U2-12, scoped by your architecture answer | — | — |

**Hard prerequisites** (A → B means A lands first):
- F1-2 → T1-3, T5-1. F1-3 → T1-4, T4-2. U3-4 → T1-1. T1-6 → T5-3. T1-7 → T5-4.
- T3-1 (with A1-1) → P1-3, P2-2, T3-5. U3-1 → T3-3, P1-1, A1-4. T3-3 and T3-4 → A1-5. T3-5 and T3-6 → T4-3. T2-1 → T3-5.
- S1-3 (Ollama slot) → S2-2. S3-1 → S2-2, S2-3, S4-4. S2-2 → S3-2, S4-4. S3-4 → S4-5. S3-5 (`truncateWords`) → S4-6, S5-2. S4-2 and S4-6 → S5-5. S1-6 → S3-6.
- P3-3 → P2 (and U1-11). P3-2 → every new CLI verb (T3-7, P1-4, P2-3, S4-8, U2-10). P3-1 → T2-3. U3-3 and U2-2 → P3-4. P1-5…P1-7 (wizard steps) → P2-4.
- U1-3 and U1-5 → U2-3. U1-10 → U2-7.

**Soft prerequisites** (cheaper rebases): T2-2 before T3-6 (the same degrade-steps slice); T3-4 before S4-3.

**Cycles:** none once #7 is applied. Without that order there is an S2-2 ↔ S3 cycle over `Clean` and `Detail`.

## Factual errors found

| Plan | Claim | Actual |
|---|---|---|
| T1 / S3 | "append" `board_hidden_sections` to registry.go | The settings page groups by consecutive section (`settings.go:553-559`), so this creates a duplicate "Behavior" section. |
| U2 | `config set` on a Lua-managed key "exits 2" (test plan) | The plan's own text says `ExitInvalidArgs`, which is **5** (`core/version.go:42`); 2 is `ExitSelectorNotFound`. |
| U2 | Sparse Save means "new registry defaults now reach users who never changed a key" | False for existing users: `NewStore` fills every default (`store.go:39-45`) and `Save` writes all values (:85). `configSet` and the TUI already froze every default into config.yaml. Needs a migration, or the claim should be dropped. |
| U2 | T1's `:bg` must use the dirty-key Save | `:bg` writes `tui-state.json` (`tui/persist.go:23`), not config.yaml. |
| S1 | "The supervisor calls refreshScreenText before Transition, so the screen is the final one" | True only on the errc path (`supervisor.go:~425`). The CompleteCh branch (:413-424) goes straight from `killAndReap` to `Transition(done)` without refreshing, so the final summary of a `C`-completed session uses a stale screen. |
| S4 | `BoardClients()` = "a TUI is connected" | Every client that Hellos gets `subscribed=true` (`handlers.go:43-57`): `rex wait`, `ls`, `status`, `attach`. That would trigger overview calls with no TUI open. Count only `Hello.Client == "rex-tui"` (`intents.go:32-34`). |
| S4 / U2 | S3's watcher is `WatchSummarySettings` | S3 names it `WatchSummaryDetail`. |
| S3 / T1 | T5 and S5 add board or header rows | They don't (see contradiction #4). |
| P2 | `spawn.go` trust call sites are at :60 and :151 | They are at :62 (`trustArgsFor`) and :146 (`trustAnswerFor`). |
| P3 | `Truncate` is at `output.go:132-141` | It is at :119-127; the byte-slicing claim is correct. |
| A1 | antigravity block is at `builtin.yaml:123-170`; `validate` is at `loader.go:155-185` | They are at 126-175 and :141. Off by a few lines. |

**Verified correct** (sample):
- PL2/T4: `:undone` toggles (`command.go:92-97` → `toggleRetire`).
- T2: `UpdateLastLine` bumps `LastEventAt` (`store.go:475-497`); exit order `SetExit` → `WriteMeta` → `Transition`.
- T3: defer order and the `hasStop` test (`spawn.go:191-201`, `server.go:582`); `ids.NewSessionID` returns a UUIDv4.
- T3/PL2/P1: Claude 2.1.286 has `--session-id <uuid>`, `-r/--resume`, `--fork-session`, `--plugin-dir`.
- A1: agy 1.5.5 has `--conversation`, `--settings`, `--prompt-interactive`, `plugin`.
- T1: `Snapshot.Filter` is always `"all"` (`handlers.go:51`) and is adopted at `model.go:262-264` and `reconnect.go:152-154`.
- U1: the init-order bug (`prompt.go:11-14` is built before `init()`'s `applyTheme` at `styles.go:111`) and the theme race (`stepThemeApply` runs inside an `emit` `tea.Cmd`).
- U3: `mergeOne` never merges `enabled_by_default`; the splash step counts nil as hidden (`splash_steps.go:174-181`); the unknown-tool error is a plain `fmt.Errorf`.
- S1: worker defects at :88, :155-174 and the single `RequestTimeout+500ms` budget; `UpdateDescription` sets `DescriptionAt` but neither the summary nor the patch carries it.
- S5: geometry `termW-14` / `termH-12` and the origin math (`attach_mouse.go:56-67`).
- P3: 28 `fs.Parse` sites; unknown command exits 1; `os.Stdin.Stat()` dereferences `fi` unchecked (`new.go:43`); `@needs` is advertised (`help.go:128`).
- S4: `claudeHookStates` maps SessionStart, Notification and Stop all to `needs_input`.
- The Go 1.26 toolchain supports `os.OpenRoot` (P1).

## Scope / risk flags

- **Breaking CLI exit codes and behaviour (P3).** Unknown command 1→5 (H12), daemon-not-running 1→4 (H9), `-h` 5→0, `rm` without a terminal now needs `-force` and declining exits 6 (H7), text starting with `-` needs `--` (H2), and the daemon rejects a relative `cwd` (H5; older clients get `bad_intent`). Call each out in CHANGELOG "Changed". Your answer is needed for H7 and H12.
- **U2 is over-scoped for the TODO line.** It makes `config.lua` a code-execution point in every rex process, CLI included, with no memory cap. Recommend phase (a) first (YAML themes plus sparse Save plus the reload hub, size M) and Lua later.
  - Alias edge case: a config with both `color_scheme` and `theme` keys needs a defined winner.
  - A downgraded rex ignores `theme`.
- **S4 may be heavier than "a feature toggle that summarizes".** It adds a protocol event, a new hook field, an overlay, a CLI and a header line. The minimum is `wait_reason` plus the header line plus `:overview`; `rex overview` can wait. Also fix the `BoardClients` cost leak above.
- **U1 changes everyone's look.** Existing config.yaml files already hold `color_scheme: default` (see the U2 freeze above), so the new default palette reaches every user.
- **U3 deletes built-in tools.** This matches "rm all other agents" and the guard covers the ab92413 failure mode. It still breaks `rex new -tool codex` (now exit 5), and S1's diagnostics and S2's fixtures still mention codex.
- **P2 `ctrl+a` trusts broadly.** It appends the *parent* of the git root (`suggestTrustDir`, `setup_wizard.go:747-774`), so one keypress trusts every sibling repo. P2's extension makes non-git folders suggest *themselves*, an inconsistent rule. Show the exact path in the UI, as planned, and confirm with you.
- **Race and data hazard in T3.** During `reserveStop`, `StopSession` calls the placeholder and returns at once. Delete can then `RemoveAll` the session dir while `startSupervisor` reopens transcript and hook files, which could recreate an orphan dir. The placeholder should set a "cancelled" flag that `startSupervisor` checks before it spawns.
- **S1's "final summary once" (`meta.final`)** must reset when T3's Reopen makes the session live again; otherwise a resumed session never gets a second final summary.
- **U2-2 read-modify-write Save** still has a race window between read and rename (no file lock). Acceptable, but document it or add a lockfile.
- **A1:** the OSC 7501 format is unverified, so A1-4…A1-6 are blocked on a live capture. vt10x logs every unknown OSC (`third_party/vt10x/str.go:137`); check the log volume. No global `~/.gemini` writes before your approval: the plan complies.
- **TODO lines that may not be fully met:**
  - T3 shows the resume id only in the detailed view, while the TODO says "in the completed section".
  - T2 adds no times to the default (simple) view.
  - S5 has no banner for shell `rex attach`.
  - P1 reads "add a step to setup" as the per-session spawn wizard, not `rex setup`.

## Consolidated user questions

**Blocking (these change what gets built):**
1. **U2 architecture:** YAML themes plus a sandboxed `config.lua` in every process (L), or YAML themes plus live reload only (M)? *Planner: hybrid. My recommendation: M first, Lua later.* Blocks U2-5…U2-9.
2. **A1 hooks:** none, (d2) a rex agy plugin, or (a) a global `hooks.json`? *Default: none.* Blocks A1-8…A1-10. And may I run **one live agy capture** (scratch folder, never committed)? *Default: yes.* Blocks A1-4…A1-6.
3. **P1 "setup":** add name, model and skills to the per-session spawn wizard (planner), or to `rex setup`? *Default: spawn wizard.* Blocks P1-5…P1-7.
4. **U1:** the new palette replaces `default` (old look kept as `classic`), with a violet accent? *Default: yes.* Blocks U1-5.
5. **P3 breaking changes:** `rex rm` needs `-force` without a terminal (H7), and unknown commands exit 5 (H12)? *Default: yes to both.* Blocks P3-6 and P3-10.

**Non-blocking (take the default):**
- **Resume (T3/T4):**
  - Resume column in the simple view's Done rows? No, detailed view only (it may conflict with the TODO's "completed section"; confirm if unsure).
  - `rex new -resume <uuid>` for pruned sessions? Later.
  - `y` key to copy the id (OSC 52)? No.
  - If `--resume` fails because Claude never saved the conversation: accept and document.
  - `rex uc` CLI alias? No.
  - Make `:done` mark-only? No, keep the toggle.
  - Send a "continue" prompt when `uc` respawns? No.
- **Board (T1/T2/T5):**
  - Section toggles last for the run; startup default via `board_hidden_sections`; `:bg` saves them.
  - Hidden sections disappear (rather than shrink to a title).
  - `zh` rather than `H`.
  - No times in the simple view; one `start→end` span column; switch `rex stats` to `ended_at` as a follow-up.
  - Search skips hidden sections and shows a `+N` hint; always case-insensitive; no match highlighting.
- **Summaries (S1/S2/S3):**
  - "Preview" means S3's selected-row preview line.
  - Keep the `gemma2:2b` default and offer `ollama pull` (cheap alternative: make `llama3.2:3b` the default).
  - Final "Done:" summary: yes.
  - Sentence case and literal `Waiting:`/`Done:` prefixes.
  - Detail default medium; reserve the preview line; apply `summary_detail` live.
- **Overview and banner (S4/S5):**
  - `overview_enabled` off by default; named "overview".
  - "Ready for review" means done-and-unmarked, or Claude's Stop with a last line that isn't a question.
  - Show it as a header line, the `:overview` overlay and `rex overview`; generate only while a TUI is open.
  - Banner default `marquee`; this session first, then the other sessions; no banner in shell `rex attach`; `::banner` not persisted; don't use `KindMarquee` yet.
- **UI and settings (U1/U2/U3):**
  - Titled square borders and a dimmed board behind overlays; table-driven commands; hide `keybind_preset`.
  - Separate `config.lua`; Lua beats YAML; rename `color_scheme` to `theme` and make config.yaml sparse; owner/mode check on `init.lua`; `rex.map` later; `auto` theme not default; `rex config edit` opens `config.lua`.
  - Delete (don't hide) gemini.
- **Spawn (P1/P2/P3):**
  - Skills library only under `~/.config/rex/skills`; no default skills for quick-spawn; no live `agy models` refresh; wizard steps skip themselves.
  - `ctrl+t` stays an explicit keypress; command named `rex trust`; defer a board `T` key.
  - Remove the `@needs` aliases from help.
- **agy (A1):** force `AGY_CLI_OSC7501=on`; full OSC messages; `trust_prompt` fallback after the capture; any hooks Unix-only; T3 owns the resume capture; doctor warns below agy 1.5.5.
- **Gap to fix in P2 (my addition):** persist `TrustFolder` so Resume re-applies trust for that session.
