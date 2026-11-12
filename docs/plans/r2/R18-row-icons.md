# R18 Row icons: replace the row text column with an icon strip

Date: 2026-11-11. Status: **draft, decisions open** (see Decisions). Base: `integration/todo` at `dae09f4`. Plan: [R18-00-plan.md](R18-00-plan.md).

## Summary

- User ask: "right now we have 2 summary modules: one at the bottom (that we are going to keep) and the one in the middle (the one we are going to remove entirely) and replace it with useful unicode symbols."
- The **middle** module is the row text column: the task (simple view) or the AI summary (detailed view) that fills the middle of every board row. It goes away.
- The **bottom** module is the `↳` preview line under the board: the selected session's full AI summary and its age. It stays, and becomes the one place a row's words are read, in both views.
- The freed width becomes a fixed **icon strip** right after the name: six slots, each a glyph and a short value, each blank when it has nothing to say. Five use data rex already has; one (spend) needs one additive wire field the usage hub already computes.
- New feature flag `row_icons` (Features, default on). Off = today's layout, byte-identical goldens.

## 1. The two summary modules

### Bottom: the `↳` preview line (keep)

- Code: `internal/surface/tui/desc_preview.go` — `previewLead = "  ↳ "` (`:23`), `previewRows` (`:33`), `previewSession` (`:57`), `renderDescPreview` (`:76`). Placed between the board and the bottom rule by `renderFullScreen` (`internal/surface/tui/tui.go:217-223`).
- Flag: `summary_preview` (Features). Docs: `docs/tui.md` → ROW COLUMNS, paragraph "Under the board, a dim preview line (`↳ …`)".
- Today it shows only the AI summary, only while some session has one, and in the detailed view only when the row cuts the text.

### Middle: the row text column (remove)

- Code: `renderRow` (`internal/surface/tui/board.go:654`) builds the cell at `:661` (`desc := rowDescStyle(...).Width(c.desc).Render(truncateWords(rowDescText(...)))`). Its content comes from `rowDescText` (`:634`) → `withLoopChip` (`loops.go:150`, the `⟳ 3/8 running` chip) → `rowDescBase` (`:639`): `rowText` (`:930`, task, or the screen's question for a session that needs you) in the simple view; `rowDescription` (`:919`, AI summary, typed-in animation, `shell 42s · ` waiting prefix) in the detailed view.
- Width: `rowCols.desc` takes whatever the fixed columns leave (`layoutFixed`, `board_columns.go:145`; `layoutFit`, `board.go:512`).

Today, 120 columns, simple view (from `testdata/golden/board_simple.txtar`):

```
  WORKING  3
 ▸ ⠋  release-notes         draft ready — which feature leads?                                               ▴ 45m   11m
   ⠋  payment-migration-v…  porting billing to the new processor — 12/14                                     ▴ 7h     2h
                            └──────────── middle: row text column (removed) ─────────────┘
  ↳ porting billing to the new processor: 12 of 14 handlers moved, webhooks next · 40s     ← bottom: preview (kept)
```

### Not it

- Header overview line `» 2 to review · <headline>` (`header.go:111`, `:119`; `overview_enabled`, off by default). It sits at the top, not the middle, and summarizes the board, not a row. Untouched.
- Attach banner `↳ <description>  code/rex · working 12m` (`attach_banner.go:37`). Lives inside the attach popup. Untouched.

Reading recorded as **D1**.

## 2. Signal inventory

Per session, what rex has or could get. "Now" = on `SessionSummary` or derivable in the TUI today. "Cheap" = the daemon already holds it; one additive field. "New" = needs a new source.

| Signal | Source | Status | Notes |
|---|---|---|---|
| state | `State` | now | the row marker (`◆ ⠋ ◷ ◌ ● ✕ ○`) |
| needs-you reason: question vs turn done | `WaitReason` (`review`/`input`), Claude hooks; `LastLine` ending in `?` for screen-only adapters | now | marker is `◆` for both today; nothing on the row tells them apart once the text goes |
| waiting on a tool + since | `WaitingOn` (`shell`/`subagent`/`web`), `WaitingSince` | now | the reason text lived in the removed column (`shell 42s · …`) |
| subagent count | `Subagents` | now | `⋔ 3` column exists |
| long-running | `WorkingSince` | now | `▴ 45m` column exists |
| last event age | `LastEventAt` | now | right-edge age column, stays |
| start→end span | `StartedAt`, `EndedAt`, `RetiredAt` | now | detailed span column, stays |
| tokens | `Tokens` (OutputBytes/4 estimate) | now | detailed tokens column, stays |
| model · effort | `ModelID`, `Effort` | now | detailed column, stays |
| resume id | `ResumeID` | now | detailed wide column, stays |
| loop membership + turn | `LoopID`, `LoopIter`, `m.Loops` | now | the `⟳` chip lived in the removed column |
| fleet | `Fleet` | now | `▍` in the indent, stays |
| marked done / retiring | `RetireAt`, `RetiredAt` | now | row dims, stays |
| unseen result (finished since you last looked) | TUI: attach time vs the time the row entered Ready or Done | cheap (TUI-only) | rex never tracks "looked at"; the TUI can, and persist it in `tui-state.json` |
| AI summary + age | `Description`, `DescriptionAt` | now | moves wholly to the `↳` line |
| task | `Title` | now | moves to the `↳` line (fallback) |
| spend (USD) | usage hub `liveSession.costs` from the Claude status-line tap (`cost.total_cost_usd`, `features/usage/hub.go:283`) | cheap | not on `SessionSummary`; Claude only (other tools report no cost) |
| prompts sent | `Prompts` (R17, `fix/ready-subagent-flap`, not merged) | cheap after R17 | turn count |
| trust | `TrustFolder` | now | set once at spawn; `skip_permissions` is global, not per session |
| skills | `Skills` | now | list, spawn-time |
| workspace | `WorkspaceID` | now | workspace board already scopes it |
| agent icon | registry `icon` (`◎ ◆ ◇ ◈ □ ◉ ▣ ▲ ◢ ⊙ ◗`) | now | claude's `◆` equals the needs-you marker |
| exit code | `ExitCode` | now | the `✕` marker already says failed |
| failed tool calls | `PostToolUseFailure` hook (already wired, writes only `working`) | new (daemon) | needs a `fail+` hook line and a counter |
| context window % | Claude status-line JSON (field not parsed today) | new (spike) | confirm the field exists before building |
| rate limit / quota | `Usage.Quotas` per tool | now (per tool) | header meter already shows it |
| undo snapshots available | `features/snapshot` ledger | new (wire) | per-folder, not per row; `turn_snapshots` is off by default |
| git branch / dirty | none | new | per-row git probe; variable-width text |
| queued prompts | none | new | no agent reports it |
| unread output bytes | `OutputBytes` | now | bytes say nothing about whether you need to look |

## 3. Candidates and verdicts

| # | Glyph | Meaning | Shown when | Format | Source | Verdict |
|---|---|---|---|---|---|---|
| 1 | `?` / `✓` / `sh` `web` `sub` | **why**: needs-you reason, or the tool it waits on | Ready rows; Waiting rows | `? asks`, `✓ review`, `sh 42s`, `web 9s`, `sub 3m` | `WaitReason`, `LastLine`, `WaitingOn`, `WaitingSince` | **keep** — top scan question on a busy board: "who is blocked on me" vs "who just finished" |
| 2 | `✦` | **unseen**: entered Ready or Done since you last attached | Ready and Done rows | `✦` | TUI | **keep** — turns the board into a review queue |
| 3 | `⟳` | **loop** turn or state | loop sessions (`loops` on) | `⟳ 3/8`, `⟳ paused`, `⟳ capped` | `LoopID`, `m.Loops` | **keep** — its old home was the removed column |
| 4 | `⋔` | **subagents** running | count > 0 | `⋔ 3` (max `⋔ 99`) | `Subagents` | **keep** (exists) |
| 5 | `$` | **spend** so far | Claude sessions with a cost | `$0.42`, `$12.3`, `$123`, `$1.2k` | new `cost_usd` | **keep** — asked for; the money signal |
| 6 | `▴` | **long-running** | working ≥ 10 min | `▴ 45m`, `▴ 2h` | `WorkingSince` | **keep** (exists) |
| 7 | `↯` | failed tool calls this turn | count > 0 | `↯ 3` | new hook line | **phase 2** (R18-2): needs daemon + hook work |
| 8 | `◔` | context window used | ≥ 70 % | `◔ 82%` | new status-line field | **phase 2** (R18-2): spike the field first |
| — | `↻` | prompts sent | — | `↻ 4` | R17 `Prompts` | cut: a count of turns does not change what you do next |
| — | `◷ 2m` / `⏱ 14m` | elapsed / timestamps | — | — | `StartedAt` | cut: `▴` covers "running long", the age column covers "last activity", the detailed span covers wall-clock; `⏱` is also emoji-prone |
| — | `⊘` | rate limited | — | — | quotas | cut: per tool, not per row; the header meter is the right home |
| — | `⚑` | folder trusted / skip permissions | — | — | `TrustFolder` | cut: spawn-time constant; belongs in the attach header |
| — | `⎇` | git branch / dirty | — | — | none | cut: needs a probe per row and variable-width text |
| — | `↶` | undo available | — | — | snapshot ledger | cut: per folder, off by default; `:undo` already answers |
| — | agent icon | tool | — | — | registry | cut: claude's `◆` collides with the needs-you marker; the filter chips and detailed model column already tell tools apart |
| — | `•` | new output bytes | — | — | `OutputBytes` | cut: noise; `✦` is the signal that matters |

Top set: **why, unseen, loop, subagents, spend, long-run** (phase 1), then failures and context (phase 2, gated).

## 4. Glyph rules

- One cell in common terminal fonts: `ansi.StringWidth == 1`, East Asian Width `N` or `Na` (not `A`, not `W`), not `Emoji_Presentation`, and no text/emoji variant pair (`⚠ ⏱ ⚙ ‼ ⁉ ⌛` are out).
- New glyphs: `✦` U+2726, `✓` U+2713, `?` `$` ASCII, `↯` U+21AF, `◔` U+25D4 — all `N`, none emoji. Reused with the same meaning: `⋔` U+22D4, `▴` U+25B4, `⟳` U+27F3.
- No clash with state markers (`◆ ⠋… ◷ ◴ ◶ ◵ ◌ ● ✕ ○`), the fleet bar `▍`, the select arrow `▸` (or the theme's), the agent icons (`◎ ◆ ◇ ◈ □ ◉ ▣ ▲ ◢ ⊙ ◗`), the logo `∴`, prompt `λ`, lead `↳`, overview `»`, rename `✎`, meter `▰ ▱`. A test enforces it (Task 1).
- Every slot has text next to its glyph, so no meaning is color-only. Color reinforces.
- Nothing in the strip animates. `reduce_motion` changes nothing here; `✦` never blinks.
- `ascii_glyphs` (Appearance, off): `✦→*`, `✓→+`, `⟳→@`, `⋔→Y`, `▴→^`, `↯→!`, `◔→%`; `?` and `$` stay. Only the strip and the legend use it in this spec.

## 5. Slots

Fixed widths. A slot that exists is always reserved, blank when empty. Order left to right after the name:

| Slot | id | Width | Text | Style token |
|---|---|---|---|---|
| unseen | `unseen` | 1 | `✦` | `accent` |
| why | `reason` | 8 | `? asks` (needs `styleNeeds`), `✓ review` (`styleDone`), `sh 42s` / `web 9s` / `sub 3m` (`styleWorking`, dim) | as listed |
| loop | `loop` | 8 | `⟳ 3/8` running; `⟳ paused`, `⟳ capped`, `⟳ done`, `⟳ failed`, `⟳ stop` otherwise | `loopStyle` |
| subagents | `subagents` | 4 | `⋔ 3` | `styleWorking`, muted when marked done |
| spend | `spend` | 6, right-aligned | `$0.42` (< $10: 2 decimals), `$12.3` (< $100), `$123` (< $1000), `$1.2k` | `fg_dim`; ≥ $5 `styleLongRun[1]`; ≥ $20 `styleLongRun[0]` |
| long-run | `longrun` | 5 | `▴ 45m` | existing tiers |

Rules per slot:

- **why.** Ready: `WaitReason == input` → `? asks`; `review` → `✓ review`; empty reason and `LastLine` ends in `?` → `? asks`; else blank. Waiting (only while `waiting_section` is on and the row is in Waiting): `sh|web|sub` + `durationAgoAt(now, WaitingSince)`. Working, Done: blank. Marked-done rows: blank.
- **unseen.** Shown on a Ready or Done row (not marked done) whose `enteredAt` (the TUI's clock reading when it saw the row enter that section) is after `seenAt` (last attach to that session, or `cc`). On the first start with no saved state every row counts as seen. `seenAt` is saved in `tui-state.json` (`seen: {id: time}`), pruned to live session ids on save.
- **loop.** Only while `loops` is on; the slot does not exist otherwise. The `withLoopChip` prefix is gone from rows (`loopChip` stays for the loops overlay).
- **spend.** Only while `usage` is on. Blank when the daemon sent no `cost_usd` (all non-Claude tools, older daemons).

## 6. Layout

Row grid with `row_icons` on:

```
indent marker  [id]  name  unseen why [loop] subagents spend longrun  ‹spacer›  [model·effort tokens span resume]  age
```

- No column reads row content (`column_layout: fixed`). The spacer is blank and takes what is left; it replaces `rowCols.desc`.
- Name widths: simple 32 / 24 / 20 (wide / normal / narrow), detailed 24 / 18 / 20. Narrow detailed = narrow simple (no detail columns), as today.
- Strip width: 32 + gaps (10) = 42 with `loops` on, 32 with it off (the loop slot and its gap go).

| Terminal width | Simple view | Detailed view (`v`) adds |
|---|---|---|
| 150+ | name 32 · strip · age | id · name 24 · model 24 · tokens · span · resume id |
| 110–149 | name 24 · strip · age | id · name 18 · model 16 · tokens |
| 61–109 | name 20 · strip · age | nothing |
| ≤ 60 (mobile) | stacked; see below | — |

Squeeze (only below the class minimum, e.g. `mobile_layout: off` at 50 columns): the spacer goes to 0 first; then drop spend, long-run, loop, subagents in that order; then the why slot shrinks to 1 cell (`?` or `✓`; waiting rows blank, the `◷` marker already says waiting); then the name shrinks to 12; the unseen slot goes last.

`column_layout: fit`: a slot exists only while some row on the board has a value for it; gives way in the order above.

**Mobile (≤ 60).** Line 1: state, name, unseen, why glyph only (`?`/`✓`), subagents, long-run, age (subagents and long-run go below 36 columns, as today). Line 2 keeps the text (task / summary), since the mobile layout has no preview line.

## 7. The kept bottom line

With `row_icons` on, the `↳` line is the only place a row's words show, so:

- Lead, first that exists: the question (needs you, `LastLine` ends in `?`) → the AI summary → `task: ` + title. Same order as the attach banner lead.
- Age suffix `· 40s` only when the lead is the summary (`DescriptionAt`).
- Shown in both views whenever the selected row has a lead; the detailed-view "only when cut" rule goes. Rows reserved: `previewRows` gates on "any session has a lead" instead of "any session has a description".
- `summary_preview` off with `row_icons` on: the line stays off; the board is names and icons. The Settings help text for `summary_preview` says so.
- Search keeps matching task, summary and question text even though rows no longer show it; the `↳` line shows the match for the selected row.

## 8. Settings and flag

| Id | Section | Type | Default | Applies |
|---|---|---|---|---|
| `row_icons` | Features | bool | on | now |
| `row_icon_set` | Appearance | string list of `unseen,reason,loop,subagents,spend,longrun` | all six | now |
| `ascii_glyphs` | Appearance | bool | off | now |

- `row_icons` off: `renderRow`, `layoutFixed`, `layoutFit`, the preview and the legend run today's code path. Every existing golden stays byte-identical. Off is inert: no `seen` writes to `tui-state.json`.
- `row_icon_set` removes slots; it cannot reorder them (order is part of the layout contract). Comma-separated like `board_hidden_sections` (`TypeString`, `ListSep: ","`); unknown ids are ignored, as there. A removed slot frees its width to the spacer.
- `cost_usd` on the wire is sent whatever `row_icons` says (it is a summary field); the TUI ignores it when the spend slot is off.

## 9. Data change: `cost_usd`

- `protocol.SessionSummary.CostUSD *float64 json:"cost_usd,omitempty"`. Nil = unknown. Additive; old TUIs ignore it.
- The usage hub sums `liveSession.costs` (+ `seedCost`) as it already does for the ledger (`recordLocked`). New hook `HubConfig.OnSessionCost func(id string, usd float64)`, called after `Sample` changed a session's total by at least $0.01, at most once per 2 s per session (status lines arrive several times a second).
- The daemon wires it to `Store.SetCost(id, usd)` (modeled on `SetSubagents`, `state/store.go:516`), which patches `cost_usd`. Persisted in `meta.json`, restored on restart.
- `usage` off: no samples, no patches; the field stays nil.

## 10. Legend and help

`helpLegend` (`help.go:97`) gains, after the existing `⋔ 3` row and only while `row_icons` is on: `✦ finished since you last looked`, `? asks: waiting on your answer`, `✓ review: turn finished`, `sh 42s: waiting on a shell (web, sub)`, `⟳ 3/8 loop turn` (only while `loops` is on), `$0.42 spend so far (Claude)` (only while `usage` is on). With `ascii_glyphs` on the legend shows the ASCII forms. `docs/tui.md` ROW COLUMNS, Column widths and Mobile layout are rewritten for the strip; CHANGELOG entry under `[Unreleased]`.

## 11. Mockups

Synthetic sessions. Spend shows only on Claude rows. `loops` on (the loop slot), `usage` on.

### 80×24 (narrow, simple; `row_density: compact` so it fits without scrolling)

```
  ∴ REX    2 need you · 2 waiting · 3 working · 2 done
────────────────────────────────────────────────────────────────────────────────
  READY  2
   ◆  dark-mode             ✦  ? asks                     $0.31               4m
   ◆  release-notes            ✓ review                   $2.10              11m
  WAITING  2
   ◷  invoice-rounding-te…     sh 42s                     $0.84  ▴ 12m       40s
   ◷  api-research             sub 3m              ⋔ 2    $1.07               2m
  WORKING  3
 ▸ ⠋  payment-migration-v2               ⟳ 3/8     ⋔ 3    $4.92  ▴ 45m        1m
 ▍ ⠋  docs-sweep                                                              7s
 ▍ ⠋  i18n-strings-extrac…                                       ▴ 2h         3m
  DONE  2
   ●  test-coverage         ✦                             $3.40               9m
   ✕  failing-job           ✦                                                  -
  ↳ porting billing to the new processor: 12 of 14 handlers moved, webhooks nex…
────────────────────────────────────────────────────────────────────────────────
  λ
  enter open · i new · cc done · tab next · ? more
```

### 120×40 (normal, simple)

```
  ∴ REX    2 need you · 2 waiting · 3 working · 2 done

────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

  READY  2
   ◆  dark-mode                 ✦  ? asks                     $0.31                                                   4m
   ◆  release-notes                ✓ review                   $2.10                                                  11m

  WAITING  2
   ◷  invoice-rounding-tests       sh 42s                     $0.84  ▴ 12m                                           40s
   ◷  api-research                 sub 3m              ⋔ 2    $1.07                                                   2m

  WORKING  3
 ▸ ⠋  payment-migration-v2                   ⟳ 3/8     ⋔ 3    $4.92  ▴ 45m                                            1m
 ▍ ⠋  docs-sweep                                                                                                      7s
 ▍ ⠋  i18n-strings-extraction…                                       ▴ 2h                                             3m

  DONE  2
   ●  test-coverage             ✦                             $3.40                                                   9m
   ✕  failing-job               ✦                                                                                      -

  ↳ porting billing to the new processor: 12 of 14 handlers moved, webhooks next · 40s
────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
  λ
  enter open · i new · cc done · tab next · ? more
```

### 120×40 (normal, detailed)

```
  READY  2
   ◆  7d4f  dark-mode           ✦  ? asks                     $0.31                     opus · high       12.3K tk    4m
   ◆  b203  release-notes          ✓ review                   $2.10                     opus · max         123K tk   11m

  WAITING  2
   ◷  3c8a  invoice-rounding-…     sh 42s                     $0.84  ▴ 12m              sonnet · medium     48K tk   40s
   ◷  9e51  api-research           sub 3m              ⋔ 2    $1.07                     opus · high         77K tk    2m

  WORKING  3
 ▸ ⠋  c4d1  payment-migration…               ⟳ 3/8     ⋔ 3    $4.92  ▴ 45m              opus · max         310K tk    1m
 ▍ ⠋  e0f2  docs-sweep                                                                  gpt-5-codex · m…    22K tk    7s
 ▍ ⠋  a913  i18n-strings-extr…                                       ▴ 2h               qwen3-coder         900 tk    3m

  DONE  2
   ●  2b7e  test-coverage       ✦                             $3.40                     haiku               88K tk    9m
   ✕  ff01  failing-job         ✦                                                       opus                           -
```

### 152 columns (wide, detailed)

```
  READY  2
   ◆  7d4f  dark-mode                 ✦  ? asks                     $0.31              opus · high               12.3K tk  11:02→         1a2b3c4d    4m
   ◆  b203  release-notes                ✓ review                   $2.10              opus · max                 123K tk  09:40→         5e6f7a8b   11m

  WAITING  2
   ◷  3c8a  invoice-rounding-tests       sh 42s                     $0.84  ▴ 12m       sonnet · medium             48K tk  11:31→         9c0d1e2f   40s
   ◷  9e51  api-research                 sub 3m              ⋔ 2    $1.07              opus · high                 77K tk  11:48→         3a4b5c6d    2m

  WORKING  3
 ▸ ⠋  c4d1  payment-migration-v2                   ⟳ 3/8     ⋔ 3    $4.92  ▴ 45m       opus · max                 310K tk  10:58→         7e8f9a0b    1m
 ▍ ⠋  e0f2  docs-sweep                                                                 gpt-5-codex · med           22K tk  11:55→                     7s
 ▍ ⠋  a913  i18n-strings-extraction…                                       ▴ 2h        qwen3-coder                 900 tk  09:51→                     3m

  DONE  2
   ●  2b7e  test-coverage             ✦                             $3.40              haiku                       88K tk  09:02→11:50    aa11bb22    9m
   ✕  ff01  failing-job               ✦                                                opus                                11:40→11:52                 -
```

The simple view at 150+ looks like the 120 one with a 32-column name and a wider spacer.

## 12. Test strategy

- **Goldens** (`internal/surface/tui/golden_test.go`, `testdata/golden/*.txtar`, `-update`): new screens `board_icons_simple`, `board_icons_detailed`, `board_icons_ascii`, `board_icons_loops` at 80×24 and 120×40, plus 150×40 for the detailed one (add a per-screen size). Plain and styled sections. The golden board gets sessions with `WaitReason` input/review, `WaitingOn`, `Subagents`, `CostUSD`, a loop, and a pinned `seen` map.
- **Byte-identical off**: with `row_icons` off, every existing golden is unchanged (`go test ./internal/surface/tui -run Golden` with no `-update` passes before and after Task 2).
- **Width invariants**: for every width 40…200 and both views, every row is exactly the content width (`ansi.StringWidth`) and slot start columns do not change when row content changes (two boards, same width, different sessions → same slot offsets).
- **Glyph test**: `TestRowIconGlyphs` — each strip glyph and ASCII fallback is one cell, EAW `N`/`Na`, not in the state marker, agent icon, select, fleet, loop-overlay or header glyph sets except the reused trio (`⋔ ▴ ⟳`).
- **Unit**: `reasonCell`, `spendText` (formats and thresholds), `unseen` rules, squeeze order, `row_icon_set` normalize, `cost_usd` patch throttle, `Store.SetCost` persistence.
- **Off-is-inert**: `row_icons` off writes no `seen` key to `tui-state.json`.

## 13. Phase 2 (R18-2, not planned here)

- **Failures `↯ 3`**: `PostToolUseFailure` writes a `fail+` line on main-thread failures; `UserPromptSubmit` resets; `Failures int json:"failures,omitempty"`. Slot width 4, after subagents.
- **Context `◔ 82%`**: spike first. If Claude's status-line JSON carries a context-used figure, parse it in `ParseStatusLine` and send `context_pct`; show at ≥ 70 % (`styleLongRun[1]`), ≥ 90 % (`styleLongRun[0]`). Width 5.

## Review focus

1. **Ready row from a screen-only adapter** (codex, agy) with no `WaitReason` and a last line that is not a question: why slot blank, unseen still works, nothing crashes on empty fields.
2. **Daemon older than `cost_usd`**: spend slot exists and stays blank; no `$0.00`.
3. **TUI restart with a saved `seen` map naming deleted sessions**: pruned on next save, no unseen flood, no growth of `tui-state.json`.
4. **`mobile_layout: off` at 45 columns**: squeeze order holds, every row exactly the width, no wrap.
5. **Theme with custom `select`/state glyph equal to a strip glyph** (`needs: "?"`): still readable because the slot always carries text; the glyph test documents the built-ins only.

## Decisions

Accepted 2026-11-11 (user): D1–D12 as recommended. Phase 1 icons confirmed: why slot, `⋔` subagents, `▴` long-running, `$` spend, `✦` finished-unseen, `⟳` loop. Phase 2 (`↯` failed tool calls, `◔` context used (was `⧗`; see 2026-11-12)) wanted, as R18-2 after the context-field spike. No timestamp or elapsed icon: `▴` plus the age column cover it.

2026-11-12 (user): the phase 2 context-used glyph is `◔` (U+25D4, EAW `N`, not emoji), not `⧗`, which reads as the waiting-icon hourglass set (`⧖ ⧑ ⧒`, `feat/waiting-spinner`). Checked against the state markers, agent icons, the strip glyphs (`⋔ ⟳ ✦ ▴ $ ↯`), the spinner frames and every waiting set (`◴◷◶◵ ⧖⧑⧒ ⣀⣄⣤⣦⣶⣷⣿ ◜◝◞◟ ⠂⡀⢀⠐ ∘◦⊚`). `◑` was rejected: EAW `A`. ASCII fallback stays `%`.

Recommendation first.

| # | Question | Recommendation | Alternatives |
|---|---|---|---|
| D1 | Which module is "middle" | The row text column (task / AI summary in every row) | The header overview `»` line |
| D2 | Row words after the change | Gone from rows in both views; the `↳` line carries question → summary → task for the selected row | Keep a short task column on 150+ only |
| D3 | Freed width | Strip left-anchored right after the name, blank spacer before the detail columns; names widen (32/24/20) | Strip right-anchored next to the age |
| D4 | Spend source | Real cost from the Claude status-line tap only (`cost_usd`); blank for other tools | Estimate from tokens × a price table, shown as `~$` |
| D5 | Spend colors | Fixed: dim, ≥ $5 warm, ≥ $20 hot | A setting for the thresholds |
| D6 | When does `✦` clear | Attach or `cc`; saved in `tui-state.json` | Also after the row is selected for 2 s |
| D7 | Why-slot wording | `? asks`, `✓ review`, `sh 42s` / `web 9s` / `sub 3m` | Glyph only (`?` `✓`), waiting reason left to the `◷` marker |
| D8 | ASCII fallback | New `ascii_glyphs` (Appearance), strip and legend only | Also state markers and spinner (later spec) |
| D9 | Choosing icons | `row_icon_set` list: remove slots, fixed order | One bool per icon |
| D10 | Phase 2 | Separate R18-2 after a context-field spike | Fold failures into phase 1 |
| D11 | Mobile | Line 1 adds `✦` and the why glyph; line 2 keeps the text | Line 2 also becomes icons |
| D12 | Flag default | `row_icons` on (pure UI, per roadmap rule) | Off until a release |
