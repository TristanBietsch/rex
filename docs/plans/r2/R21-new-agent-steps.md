# R21 New-agent steps: a full-screen stepped flow with a summary

Status: spec draft 2026-11-13. User picks (2026-11-13): **shell-transcript** step style, **agent first** order, **replace both** the R16 Compose page and the popup wizard (flag `new_agent_page` removed). Builds on R16 (state, context, import, preview, trust, launch) and R04 (page frame).

## Summary
- `n` / `:new` opens a full-screen page, `∴ REX › NEW AGENT`, that walks **seven steps** one at a time: `agent`, `model`, `skills`, `task`, `context`, `trust`, `summary`. The summary is the last step; `enter` there launches.
- Each step reads like a line in a shell installer (`gh repo create`, `npm init`): finished steps stack at the top as one terse line each, `✓ key  value`; the current step is a `› key  question` line with its body under it, indented, using every row left. Steps not reached yet are not drawn; the header says `step 3/7`.
- No step rail, no narrow tab strip, no Compose form, no Agent screen, no popup wizard. One layout at every width.
- Everything R16 built underneath stays: `NewAgentState`, the composer, paste chips, `@` completion, the import picker, the preview, trust rules, `spawnctx`, the launch path and the in-memory draft.

## Why
- The R16 Compose page puts agent, task, context, trust and launch on one screen; at 80×24 the task box gets 1–3 rows and the context list 1 row. Picking a model means a second screen with three panes.
- Two flows (page + popup behind a flag) double the goldens and keys to keep in sync.
- The user wants one thing per screen, with room, and a review before launch.

## Goals / non-goals
- Goals: one step per screen with the whole body for it; done steps visible as a transcript; summary that shows everything the launch sends; keyboard-first; works at 80×24 and under 60 columns with the same layout.
- Non-goals: new context kinds, presets/templates (R01), mouse click targets, changing what the launch sends.

## Steps

| # | Key word | Body | Done line (`✓ key  value`) | Skipped when |
|---|---|---|---|---|
| 1 | `agent` | installed agents (brand glyph + name), then dimmed `not installed` rows (R16 D7: `enter` → Onboarding) | `✻ Claude Code` | never |
| 2 | `model` | `/` filter line, model list (detected first), then `effort  low medium high [max]` row | `opus · effort max` | never (one model: list of one) |
| 3 | `skills` | `[x]` list, `space` toggles | `go-testing, release` or `none` | `spawn_skills` off, empty library, or tool has no `skill_args`: done line `– skills  not available` (dim), never focused |
| 4 | `task` | the composer, full width, `bh - transcript - 3` rows (min 3, no max); `@` completion under it | first line of the task, `(+N lines)` | never |
| 5 | `context` | item list (R16 rows) with the total flush right; empty state names `ctrl+o`, `@`, paste | `3 items · 7.1 KB` or `none` | never |
| 6 | `trust` | three choices as a radio list: `( ) ask: the agent will ask`, `( ) this session only`, `( ) always: saves ~/dev/rex to auto_trust_dirs`; a covered folder shows one `trusted (auto_trust_dirs)` line; a refused one its reason and only `ask` | `this session` / `always` / `ask` / `trusted` | never |
| 7 | `summary` | review list (below) and the launch row | — | never |

### Summary step
The transcript is replaced by the full review; each row is selectable.

```
  ∴ REX › NEW AGENT    ~/dev/rex                          step 7/7
────────────────────────────────────────────────────────────────────
  › summary  review, then launch

      agent    ✻ Claude Code
      model    opus · effort max
      skills   go-testing
    ▸ task     the auth test is flaky on CI, see the paste. fix it…
               (+2 lines)
      context  3 items · 7.1 KB
                 file   internal/auth/auth_test.go          4.2 KB
                 dir    internal/auth/testdata/           12 files
                 paste  paste 1  "--- FAIL: TestLogin"  38 lines
      trust    this session only
      prompt   2.0 KB of 96 KB
      slug     cc.opus.the-auth-test-is-flaky-on-ci

      $ rex new -tool claude -model opus -effort max -skill go-testing -trust …

      ▸ launch
────────────────────────────────────────────────────────────────────
  enter launch · j/k row · e edit row · esc back · ? help
```
- `j`/`k` walk the rows (agent … trust, launch); `enter` or `e` on a step row jumps to that step (and a later `enter`… returns through the steps, see Keys); `enter` on `launch` (the default cursor) launches.
- The `$ rex new …` line is the equivalent CLI call, dimmed, for the fields `rex new` has flags for (`-tool -model -effort -skill -trust`); context and `always` trust have no flag, so they are omitted and the line ends in `…` when anything is left out. Display only, never run, never copied anywhere.
- Prompt size over the cap shows `! prompt` in the warning style (R16 rule); a launch error lands under the launch row (R16 `naErrLines`).

## Layout (80×24, step 3)

```
  ∴ REX › NEW AGENT    ~/dev/rex                           step 3/7
────────────────────────────────────────────────────────────────────
  ✓ agent    ✻ Claude Code
  ✓ model    opus · effort max
  › skills   pick any (optional)

      [x] go-testing   table-driven tests and goldens
      [ ] release      cut a release branch




────────────────────────────────────────────────────────────────────
  space toggle · enter next · esc back · ? help
```
- Transcript keys are padded to 9 columns (`summary` + 2); values truncated with `…` to the width.
- Body indent: 6 columns (4 under 60 columns). A blank row between the `›` line and the body.
- Notices (remembered agent gone, stale-root files), errors and the R16 pending-confirm line render right under the `›` line.
- Import picker and preview open in the body area under the transcript (the transcript stays, the body is theirs); `esc` returns to the step.
- Header right word: `step N/7`, or `starting…` while launching. Skipped steps still count in N/7 (stable numbering, so `1`–`7` jumps match).

## Keys

| Key | Where | Action |
|---|---|---|
| `n`, `:new` | board | open the page on the draft's step, else **task** when an agent is remembered (last launch or `default_spawn_tool`), else **agent** |
| `enter` | steps 1–6 | confirm and go to the next not-skipped step |
| `enter` | task editor | **next step** (was launch, R16 D2; the summary is now the launch point). `ctrl+j` / `alt+enter` newline |
| `enter` | summary | launch on the launch row; jump to the step on a step row |
| `esc` | any step | back one step (closing `@` list / filter / rename first); on step 1, or the first step the page opened on, close the page, draft kept |
| `tab` / `shift+tab` | anywhere | next / previous step (no confirm needed) |
| `1`–`7` | not typing | jump to a step (skipped: no-op) |
| `ctrl+s` | anywhere | launch now (R16 kept; skips the summary) |
| `ctrl+o` | task, context | import picker |
| `ctrl+t`, `ctrl+a` | anywhere | trust toggles (P2), unchanged |
| `q` | not typing | close, draft kept |
| `j`/`k`, `gg`/`G`, `ctrl+d`/`ctrl+u` | lists | shared navigator (nav.go) |
| `/` | model | filter |
| `h`/`l` | model | effort |
| `space` | skills, trust | toggle / pick |
| `x`, `r`, `e`, `enter` | context | remove, rename, edit paste, preview |
| `?` | not typing | help filtered to the new-agent group |

Removed: `h`/`l` rail focus, `[`/`]`, `ctrl+g` (agent is step 1; `ctrl+g` maps to "go to agent step" for muscle memory), `i`/`a`/`A`/`I` (the task step always types).

Returning from a summary jump: after editing a step reached from the summary, `enter` goes straight back to the summary (`st.ReturnToSummary`), not through the following steps.

## Removal

- `new_agent_page` flag: settings registry entry, `featureOn` checks, `keys.go` `Flag:` fields, docs, Lua config docs, settings goldens. A stored `new_agent_page = false` in a user's settings is ignored (unknown keys are already tolerated — verify).
- Popup wizard: `openWizard`, `FocusWizard`, its view/keys, `naSeedWizard`, `closeNewAgentIfOff`, and goldens `wizard*.txtar`. Helpers the page shares (`WizardState` selection helpers, `quickChoice`, `presetEffortDefault`, `effortOptions`, `chosenSkills`, trust offer and request) stay, moved/renamed only if it does not grow the diff; renaming is a follow-up.
- R16 Compose code: `naRail`, `naStrip`, `naNarrowBody`, `naForm`, the `naSection` rail model, Agent screen panes (`naAgentPane`, three-pane rendering). Reused pieces: model filter, effort move, skills toggle, item rows, editor box, trust offer.
- Exact entry-point and test list: see the removal map produced during review (appendix below).

## State changes
- `naSection` → `naStep` (`naStepAgent … naStepSummary`, `naStepCount = 7`). `NewAgentState.Step`, `.Opened` (first step shown, for `esc` close), `.ReturnToSummary`, `.SumRow` (summary cursor).
- `Screen` keeps `naPreview` and `naImportScreen`; `naCompose`/`naAgentScreen` collapse into `naSteps`.
- Draft (D4) keeps `Step` instead of `Section`.

## Accessibility
- Every state has a glyph and a word: `✓` done, `›` current, `–` skipped, `▸` cursor, `[x]`, `(•)`, `[bracketed]` effort, `!`/`✗`.
- `theme ansi` and `high-contrast` goldens for the task step and the summary.

## Test strategy
- Goldens at 120×40, 80×24, 50×20 (plain + styled): `newagent_step_agent`, `_model`, `_model_filter` (200 models), `_skills`, `_task` (empty and filled), `_context`, `_trust`, `_summary`, `_summary_err`, `_import`, `_preview`, `_loading`, `_frame`, `_ansi`, `_highcontrast`. Old `newagent_*` and `wizard*` goldens deleted.
- Keys: enter walks 1→7 skipping skills when unavailable; esc walks back and closes on the opened step; `enter` in the editor goes next, never launches; a bracketed paste never advances; summary row jump + return; `ctrl+s` from any step launches; `1`–`7`; draft restores step.
- Launch, context, import, trust and `spawnctx` tests from R16 keep passing unchanged where they do not drive Compose keys; tests that drive the rail are rewritten against steps.
- `make check` (coverage floor) green.

## Decisions

| # | Question | Recommendation |
|---|---|---|
| E1 | `enter` in the task editor | **Next step** (summary is the launch point); `ctrl+s` still launches immediately. Supersedes R16 D2. |
| E2 | Where `n` lands | **task** when an agent is remembered, else **agent** (keeps R16 D1's fast path: `n`, type, `ctrl+s`). |
| E3 | Show steps not reached yet? | **No** (shell transcript); `step N/7` in the header. |
| E4 | Equivalent `rex new` line on the summary | **Yes**, display only. |
| E5 | Skipped skills step numbering | **Stable 7 steps**, skipped shown as `–`. |

## Appendix: removal map (2026-11-13)

- Only entry: `openNew` (callers `update.go` board `n`, `keys.go` `runNew`). Becomes `openNewAgent` directly.
- Delete: `openWizard`, `updateWizardKey`, `renderWizard` and wizard-only helpers in `wizard.go` (`wizardStep`, `stepApplies`, `next/prevWizardStep`, `moveWizard`, `updateWizardDescribeStep`, `closeWizard`, `(*WizardState).request`, `wizardLaunchCmd`, `wizOption`, `wizSkillLines`, `wizInputBoxLines`, `wizardFooter`, width consts); `wizardTrustKey`, `wizardTrustRequest`, `wizTrustLines` in `wizard_trust.go`; `naSeedWizard`, `closeNewAgentIfOff`; `FocusWizard` (model.go, update.go, tui.go, events.go, swap.go); `Model.Wizard`; wizard branches of `refreshPickers` (tool_pickers.go).
- Keep (shared): `WizardState` selection fields + `selectedTool/Model`, `currentEffort`, `effortOptions`, `presetEffortDefault`, `skillsApply/chosenSkills/toggleSkill`, `loadWizardSkills`, `loadToolsStrict/loadUserTools/loadToolsOrBuiltin`, `visibleTools`, `existingSlugs`, `playNav`, `pendingSpawn/ignoredBy`, slug helpers, `padOrTrim`; trust offer/request/lines in `wizard_trust.go`; `runTrustCmd`. Renames are a follow-up.
- `setup_wizard.go` (`rex setup`) is independent: untouched.
- Flag: `settings/registry.go` row + `apply_test.go`; `keys.go` `Flag:` fields; `golden_test.go` `wizardFlag`; docs (`settings.md`, `tui.md`, `slash.md`, `agents/modules/tui.md`, CHANGELOG entry for the change).
- Tests: delete `wizard_model_test.go`, `wizard_skills_test.go` (move helpers still used by newagent tests), wizard cases in `picker_fallback_test.go`, `tool_pickers_test.go`, `command_test.go`, `paste_test.go`, `screensaver_test.go`, `snapshot_test.go`, `golden_test.go` and goldens `wizard*.txtar`; port `wizard_pending_test.go`'s `ignoredBy` case to the page; split `wizard_trust_test.go` / `wizard_trust_failclosed_test.go` keeping `:trust` tests; swap `FocusWizard` for `FocusNewAgent` in focus-list tests.

## Iteration 1: review fixes (2026-11-13)

These rules override anything above that disagrees.

### Trust (P2 safety)
- T1. Each trust row shows the exact resolved folder it trusts under it (`trustOption`: `SessionDir`, `AlwaysDir`, wrapped), never only the `~` entry.
- T2. `AlwaysDir == ""` (outside home): `always` row dimmed with the reason, skipped by j/k and space.
- T3. Only `space`, `ctrl+t`, `ctrl+a` change the choice. The cursor opens on the current choice. `enter` never picks: it goes next. Launch writes `auto_trust_dirs` only for an explicitly chosen `always`.

### Step rules
- S1. Tools loading (`naToolsLoading`): agent done line and agent body show the spinner text; `enter` on step 1 is a no-op until the list lands.
- S2. `refreshNewAgent` fallback sets `Notice`; if the current step becomes skipped, move to the next non-skipped step.
- S3. A `Notice` at open (remembered agent gone) opens on the agent step; confirming step 1 clears it.
- S4. E2 rewritten: open on the draft's step, else agent when `Notice` is set, else task.
- S5. Stale-root notice and path launch errors render on the context step; a `ctrl+s` or summary launch that fails on a path jumps to context; other launch failures land on summary under the launch row. Error words name the step: "go to context (5), x removes it".
- S6. Launching: from any step; spinner under the `›` line, header right `starting…`; only `esc` (close, draft kept); failure per S5.
- S7. Context step: `enter` on an item previews; a final `next ›` row under the list takes `enter` to go next; with no items, `enter` goes next. `tab` always goes next. Footer names both.
- S8. Model filter: typing filters; `enter` keeps the filter and leaves typing (second `enter` goes next); `esc` clears typed text, then a kept filter, then goes back a step.
- S9. A model with no effort: no effort row, done line drops `· effort`. A tool with no models: model step skipped like skills (`–`).
- S10. Successful `ctrl+o` add from task returns to context (R16 behaviour); from context stays on context.
- S11. Picker and preview: transcript and `›` line stay; they own the rest of the body; their row math uses that height (fixes preview scroll clamp, `naPreviewScroll`).

### Keys
- K1. `esc`: closes innermost (completion, filter, rename, picker, preview) first; then with `ReturnToSummary` set returns to summary; else on a step ≤ `Opened` closes the page (draft kept); else back one non-skipped step.
- K2. `tab` / `shift+tab`: completion accepts first; ignored in filter, rename, picker query; no wrap; clears `ReturnToSummary`.
- K3. `ReturnToSummary`: after an agent change it walks model, then skills (if it applies), then summary; after a model change, skills (if it applies) then summary.
- K4. Paste: on task inserts (R16 chips); on agent, skills, context, trust, summary jumps to task (clears `ReturnToSummary`) and inserts; model filter types it flat; picker query as today.
- K5. Task step: `ctrl+e` opens `$EDITOR` on the task; `ctrl+u` clears the line, and on an empty line clears the task text only (no longer drops context, trust, skills; `:new!` still clears the whole draft). `?` works on an empty task. E1 supersedes R16 D2 and its 2026-11-12 vim-mode addendum: the task step has no navigation mode.
- K6. Kept aliases: context `d`, `delete` (remove); effort `←/→`, `+/-`; `ctrl+g` = go to agent step.
- K7. Mouse wheel = `j/k` on the step's list (agent, models, skills, context, trust, summary rows); task: no-op; preview scrolls; picker unchanged.

### Layout budget
- L1. `bh = h - pageChromeRows`. Task rows = `bh - len(transcript) - 4 (›, blank, 2 box edges) - notices - errors - completion`, min 1; completion capped to what is left after a 1-row box.
- L2. Summary: rows windowed with the shared navigator on `SumRow`; the launch row and errors pinned to the bottom. Context sub-rows are display only (not selectable); collapsed to `N items · X KB` under 60 columns or when they don't fit. The `$ rex new` line drops first when short on rows.
- L3. Every step body is windowed (listView) to the rows left; nothing is cut by `composePage`.

### Summary CLI line
- C1. `$ rex new -tool T -model M [-effort E] [-skill S]... [-trust] "<task first line>"`; values with spaces or shell metacharacters single-quoted; task truncated with `…` to the width. Context or `always` trust present: ends with `  # + context` / `# + always trust` comment. Display only.

### Settings
- Z1. A stored `new_agent_page` stays harmless: YAML ignores it; add it to a Lua removed-keys allowlist so `config.lua` does not warn. Docs note the removal.
