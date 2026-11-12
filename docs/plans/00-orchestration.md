# Orchestration: TODO `***` items

Branch: `docs/todo-priorities` (local only, nothing pushed). Date: 2026-11-10.

This file is authoritative. Where it disagrees with a per-task plan, this file wins.
Per-task plans: `docs/plans/<ID>-*.md`. Independent review: `00-review.md`.

## Scope (17 tasks + 1 refactor)

| ID | TODO line | Size |
|---|---|---|
| F1 | (prerequisite) board section ids, row filter, chord table | S |
| T1 | filter kanban steps / hide completed | M |
| T2 | start / complete timestamps | M |
| T3 | `--resume` id column + resume action | L |
| T4 | `uc` undo complete | S |
| T5 | search | M |
| S1 | fix ollama preview | L |
| S2 | better summary prompt | M |
| S3 | no "claude is…", detail setting | M |
| S4 | master agent (overview) toggle | L |
| S5 | marquee banner above agent, toggle | M |
| U1 | remake UI, colors, help, settings | L |
| U2 | settings hackable + themes | M (YAML only, see D1) |
| U3 | rm other agents from settings | DROPPED (D13); step 1 guard merged |
| P1 | setup step: name, model, skills | L |
| P2 | rm "yes, i trust" | M |
| P3 | harden the CLI | L |
| A1 | antigravity parity | M (no hooks, see D7) |
| K1 | opt-in skip-permissions setting (D9) | S — merged ca7a957 |
| U4 | cmd for thick colored window border | M — wave 7 lane B (after U1) |
| U5 | fixed-width columns, breakpoints, mobile layout | M+ — wave 5 lane A after P2 (U5-5 after S5-1) |
| U6 | agy logo in attach window (vt10x REP/tabs, spawn size, size clamp) | S–M — U6-1..3 wave 3 lane B; U6-4,5 wave 5 lane B after S5-1 |
| P4 | CLI output look, -o formats, --agent mode, error log (plan pending) | L |

Out of scope: unstarred items, incl. "(epic) insert mode auto chooses model".

## User decisions (2026-11-10)

| # | Decision | Answer |
|---|---|---|
| D1 | U2 architecture | YAML themes + one live settings reload now (U2 option (a), size M). `config.lua` is a later follow-up. Drop U2 steps 5–7 Lua parts, keep 1–4, 8–11 minus Lua. |
| D2 | A1 live agy capture | Yes, one run. Temp folder, tiny prompt, raw bytes stay in the session scratchpad, never committed. |
| D3 | S3 preview line | Yes, both views, only while some session has a description. |
| D4 | P3 breaking CLI changes | Accept all (H7 `rm` needs `-force` without TTY, H12 unknown command exits 5, `--` before text starting with `-`). CHANGELOG "Changed" note for each. |
| D5 | U1 palette | New palette becomes `default`, violet accent `#B392F0`; old look ships as `classic`; add `high-contrast`, `ansi`. |
| D6 | P1 "setup" | Per-session spawn wizard (`n`, `:new`) + `rex new -name/-skill`. Not `rex setup`. |
| D7 | A1 hooks | None. OSC 7501 per-session env + footer fallback. Never write `~/.gemini`. Drop A1 steps 8–10. |
| D8 | P2 `ctrl+a` scope | Trust the repo's own folder (git root, or the folder itself if not a repo). NOT the parent. `SuggestTrustDir` changes accordingly; show the exact path. |
| D9 | `--dangerously-skip-permissions` (user commit 29ca4e9) | Make it opt-in: new `skip_permissions` bool setting (Behavior section, default **off**, help text warns). Remove the flag from the builtin `command` arrays; registry gains `skip_permissions_args` per tool (claude, agy: `["--dangerously-skip-permissions"]`); `buildArgv` appends them only when the setting is on. Task **K1**, lane B, after T2-1. Doc in SECURITY.md + settings.md. |
| D10 | New TODO lines from 29ca4e9 | Top-level unstarred lines: out of scope; U3 and D1 stand. **Correction:** the 3 lines added under `*** ## UI / settings` ARE in scope → U4 window border cmd, U5 fixed-width responsive columns + mobile layout, U6 agy logo centered in the attach window. |
| D11 | P4 CLI output | agy command-output structure (Usage-first, Title-case sections, aligned, no banners, color for meaning); agent mode = 1 line per record + `ok` trailer, errors one stderr line; auto agent mode when CLAUDECODE=1 and stdout not a TTY (REX_AGENT=0 disables); `-json` stays legacy alias, `-o json` versioned; pipes default `txt`; viewer `rex errors`; error log on, 1 MiB + 1 backup, opt-out `cli_error_log`. P4-1..5 wave 2 lane C first; P4-6..13 wave 4 lane C after P3-11. |
| D12 | Gates | Go toolchain: user upgrades (`brew upgrade go`); until then `make vuln` stdlib advisories are non-blocking. Coverage: no-regression rule for audio, cli/core, cli/inspect, cli/session, cli/setup (must not drop below base 61.2/43.6/36.3/27.9/60.6); all other floors enforced. |
| D13 | Scope after TODO.md edit (2026-11-11) | **U3 dropped** (keep merged retired-tool guard 76c58a9; no agent removal, no `filterToolIDs`; T1 validates `:filter <tool>` against the existing `filterTools` list ∪ tool ids present on the board; U3-5 unknown-tool bad_intent dropped). Kept: S4, S5, P1, P2, P3, P4, all T*, S1–S3, U1, U2, U4–U6, A1. Progress tracked here only; TODO.md is the user's list (tick a line only when fully done). |
| D14 | U4–U6 (2026-11-11) | U4: border off by default (`:border` per run, settings persist), random = stable per launch folder (`:border next` re-rolls), block frame `█`/`▀▄`, Pantone-inspired ids, hidden on splash/screensaver, `:border` not persisted. U5: breakpoint fixed widths (wide/normal/narrow/mobile), always-reserved sometimes-empty columns, mobile auto ≤60 cols with 2-line rows + attach sheet, no width knobs. U6: keep inherited TERM, fix vt10x (REP capped, tab stops on grow, DECST8C), spawn at popup size, clamp Resize/NewSession sizes (fixes daemon OOM via 65535×65535 Resize), live check folded into A1's D2 capture, hand-written fixtures. |
| D15 | Feature flags (2026-11-11) | Every round-1 **feature** gets an on/off toggle in Settings (same rule as round 2, `docs/plans/r2/00-roadmap.md` → "Feature flags"). Bug fixes, refactors and CLI hardening get none. New task **FF0** (lane C, first in wave 3) adds `SectionFeatures` ("Features", after Behavior), `settings.FeatureOn(store, id) bool`, and a registry test (every Features row is bool/enum with help text). Off = inert: no goroutine, subprocess, network, file write or argv addition; keys unbound; `:`/CLI commands answer `off: enable in Settings → Features (rex config set <flag> true)`, CLI exit 5. Each flag's owning step adds an off-is-inert test. Existing toggles keep their id and section. Table: "Feature flags" below. |
| D16 | skip_permissions scope (security review, MEDIUM) | Task **K2**: append `skip_permissions_args` only when the session folder is trusted (under `auto_trust_dirs`, or per-session trusted once P2 lands). Otherwise omit and log `skip_permissions ignored: folder not trusted`. `trustDecision` gains `Trusted bool`. Tests + SECURITY.md/settings.md wording. Lane B, next dispatch. |
| D17 | CLI error log content (after 2 privacy reviews) | **Structure only**: time, version, os, pid, known cmd/subcommand (else "?"), registered flag NAMES, arg count, format, tty flags, exit code, kind, Go error type chain, optional allowlisted error ID. Never message text, hints or argument values. |
| — | Agents | claude + antigravity visible; echo hidden (tests). Others deleted, with U3's retired-tool guard. |
| — | `/ob` | Ignored. |
| — | Remote | Keep local. No push, no PR. |

Non-blocking questions take the planner defaults listed in `00-review.md` → "Non-blocking".

## Overrides from the review (resolve contradictions)

1. **Spawn refactor first (T3-1 + A1-1 combined).** One `spawnSpec` struct (`Tool, Model, Effort, CWD, Trust trustDecision, Resume *resumeArgs, PluginDir, HookFile, SessionID`); `buildArgv(spawnSpec)` and `childEnvFor(spawnSpec)` are pure. Argv order: command, model, effort, trust, resume, skills, hooks, then `--` prompt. P1/P2/T3/A1/U3 build on it.
2. **Persist `TrustFolder`** (P2) in meta; T3 `handleResume` re-applies trust and re-adds `--plugin-dir` when `Skills` is set.
3. **`:resume` = T4's `undoComplete`** (same as `:uc`). T3-6 adds only `:rid` + resumer wiring.
4. **Resume ids**: registry block `{args, new_args?, capture_regex?}`, placeholder `{id}` (single argv element, not whitespace-split — document vs effort `{value}`). Validator = T3 `ValidResumeID`; lowercase on ingest.
5. **Row accounting**: `headerHeight = 1 + chip row (T1 predicate) + overview line (S4, after chips) + backend banner`; `boardBottomRows(m) = 3 + previewRows(m)` (S3). T5 and S5 add no board rows.
6. **Settings order** (sections must stay contiguous; add a registry test): `board_hidden_sections` goes in Behavior after `screensaver_keep_awake`. Summary section: `summary_enabled, summary_model, summary_detail, desc_animation, overview_enabled, attach_banner`.
7. **One watcher**: S3-4 builds generic `boot.WatchSettings(ctx, path, every, apply ...func(*settings.Store))`; S4-5 adds an applier; U2-8 wraps it as `SettingsHub` + SIGHUP/reload.
8. **Summarizer order**: S3-1 (`Clean`) → S2-1 → S2-2 → S2-3 → S3-2. S2 owns `Detail` + caps; S3 adds `ParseDetail`/`SetDetail`. `cleanResponse` stays as a shim until S3-7.
9. **`filterToolIDs(m)`** (U3-4) lands before T1-1; F1's test uses a literal list.
10. **Hook edits**: T3-4 (`sid`, both SessionStart branches) before S4-3 (`wait_reason`); `sid` lines must not clear `wait_reason`. Adapter coverage floor 98 %.
11. **Supervisor**: type-assert `SubagentCounter`, `ResumeIDReporter`, `WaitReasoner` once before the tick loop; A1 `OutputObserver` in the reader after `screen.Write`. Exit order on both errc and CompleteCh: `refreshScreenText` → capture_regex → clear side-channels → `SetExit`/`WriteMeta` → `Transition` → `resume_id` patch. Add the missing `refreshScreenText` to CompleteCh (S1 final summary depends on it).
12. **Command table early**: U1-7 (table-driven `:` commands + bindings, no visual change) lands right after F1; every later command adds a table row with help text. U1 goldens (U1-1) wait for the final UI wave.
13. **Sparse Save early**: U2-2 (read-modify-write, dirty keys only) lands in wave 1. T1 `:bg` is unaffected (writes `tui-state.json`). Drop U2's claim that new defaults reach existing users. U2 test: Lua-managed exit code is moot (no Lua); managed-key errors use ExitInvalidArgs = 5.
14. **Unknown tool**: one helper, `bad_intent`, same message for NewSession (U3-5) and Resume (T3).
15. **One validator** for `auto_trust_dirs`: `Store.Set` → `Setting.Normalize` (P3-H1); U1-11 and P2 call it.
16. **doctor append order**: A1 `checkAgyVersion` → U3 `checkRegistryWarnings` → P2 `checkAutoTrustDirs`.
17. **S4 viewer gate**: count only `Hello.Client == "rex-tui"`, not every client.
18. **T3 delete race**: the `reserveStop` placeholder sets a cancelled flag that `startSupervisor` checks before spawning (no orphan session dir).
19. **S1 `meta.final`** resets on T3 `Reopen`.
20. **T4**: F1's `chordHandler(m, second)` already supports `u`→`c`; no `second` field.
21. **U1 `color_scheme`**: U1-5 extends options; U2-3 renames to `theme` (alias; if both keys exist, `theme` wins). CLI `core.Styles()` reads `theme` after U2-3.

## Build waves (3 lanes, max 3 builders, local worktrees)

Lane A = TUI, B = daemon/registry/protocol, C = CLI/settings/summarizer. Steps within a lane run in order; a lane waits on cross-lane prerequisites (see `00-review.md` → Hard prerequisites).

| Wave | Lane A | Lane B | Lane C |
|---|---|---|---|
| 1 | F1-1..3 → T4-1 (`:undone` fix) → U1-2 (arrow color, theme race, NO_COLOR cursor) | T3-1+A1-1 (spawn spec) → U3-1 (retired-tool guard) → T2-1 (`ended_at`) → K1 (opt-in skip-permissions) | P3-1 → P3-2 (`ParseArgs`, `-h`) → P3-3 (H1) → U2-2 (sparse Save) |
| 2 | U3-4 → U1-7 (command table) → T1-1..8 → T2-2 | U3-2,3,5,6 → T3-2..4 | S1-1 → A1-3 (agy fixtures) → S1-2..4 → S3-1 → S2-1,2 |
| 3 | FF-T1 → T5-1..5 → T4-2 | T3-5..7 → T2-3 → T4-3 → A1-2 | FF0 (Features section, D15) → S1-5,6 → S2-3,4 → S3-2..7 → S1-7, S2-5 |
| 4 | P1-1..8 | S4-1..9 | P3-4..11 |
| 5 | P2-1..6 | S5-1..6 | A1 capture (D2) → A1-4..7 |
| 6 | U1-1, U1-3..6, U1-8..12 (single builder) | — | — |
| 7 | U2 (YAML scope per D1) | — | — |

## Feature flags (D15)

FF0 must merge before any step below that adds a flag. Steps already merged before D15 get a small retrofit step (`FF-<task>`) in the same lane.

| Flag | Feature | Default | Lands in | Off means |
|---|---|---|---|---|
| `board_filter` | T1 chips row, `:filter`, `:hide` / `:show` | on | FF-T1 (lane A, wave 3, after T1 merges) | no chip row, commands refuse; `board_hidden_sections` ignored |
| `timestamps` | T2 start/complete times (TUI detail column, `rex ls` columns) | on | T2-3 (also gates T2-2's column) | times hidden; `ended_at` is still recorded (data, not a feature) |
| `resume` | T3 resume ids, resume column, `:rid`, Resume intent | on | T3-6 (also gates T3-2..4 capture) | no `--session-id` preassign, no capture, no column, Resume → `bad_intent` "resume is off" |
| `undo_complete` | T4 `uc`, `:uc`, `:resume` | on | T4-2 | chord and commands refuse; the `:undone` fix (T4-1) stays, it is a bug fix |
| `search` | T5 `/` search and `:search` | on | T5-1 | `/` unbound, `:search` refuses |
| `summary_preview` | S3 preview line under the board | on | S3 step that adds the line | no preview line, row accounting as before |
| `live_reload` | U2 settings poller / SettingsHub auto-apply | on | U2-8 | no polling; `rex reload`, `:reload` and SIGHUP still reload by hand |
| `spawn_skills` | P1 skills step in the wizard + `rex new -skill` + `--plugin-dir` | on | P1 step that adds skills | skills step hidden, `-skill` refuses, no `--plugin-dir` argv |
| `cli_agent_auto` | P4 auto agent mode when `CLAUDECODE=1` and stdout is not a TTY | on | P4 step that adds auto detection | only explicit `--agent` / `-o agent` / `REX_AGENT=1` |

Existing toggles stay where they are (same id, same section); Settings search (U1) finds them:
`overview_enabled` (S4, off), `attach_banner` (S5), `summary_enabled` (S1–S3), `skip_permissions` (K1, off), `window_border` (U4, off), `column_layout` `fixed|fit` (U5), `cli_error_log` (P4), `auto_trust_dirs` (P2, empty = off), `screensaver_enabled`, theme `classic` (U1's old look).

No flag (fixes, refactors, hardening): F1, U1-2, T4-1, P3, U6, A1 (tool support; the tool is enabled per tool in Settings → Onboarding), S2 (internal, under `summary_enabled`), U1 help/settings content.

## Per-task pipeline (every builder)

1. Worktree off the integration branch (`feat/<id>-<slug>`), read: this file, `00-review.md`, the task plan, `CONTRIBUTING.md`, `docs/STRUCTURE.md`.
2. Test first. One commit per plan step; each builds and passes `make check` (vet, race tests, both lints, coverage floors, govulncheck). Raise floors in `tools/coverage/baseline.txt` when coverage rises.
3. Docs + CHANGELOG `[Unreleased]` in the same step as the behavior change. Conventional Commits, attribution trailer.
4. Never: `make install`, `./install.sh`, writes to `~/.claude`, `~/.gemini`, `~/.config/rex` outside `t.TempDir()`/`t.Setenv("HOME")`, push, PR.
5. Review: `feature-dev:code-reviewer` on the branch diff; builder fixes; max 2 rounds.
6. Verify (orchestrator): `make check` re-run, then `dev/sandbox.sh` manual run of the changed flow in the real TUI.
7. Merge into the integration branch in wave order; next branch in the lane rebases.
8. Tick the TODO.md line when its last step merges.

## Status (updated by orchestrator)

| Wave | State |
|---|---|
| 1 | **Merged** into integration/todo @ 2c0be13: F1-1..3, T4-1, U1-2 (+review fixes), T3-1+A1-1 spawn spec, U3-1 guard, T2-1 `ended_at`, K1, P3-1/2/3 (H11, H2+H3, H1), U2-2 sparse Save (+wizard reset fix). `make check` green (lint 0, vuln clean, floors pass). Sandbox TUI check: board, arrow color, `:undone`, `cc`, help OK. |
| 2 | **Merged** @ 9b35370: U1-7 command table, T1-1..8 filters/hide sections, T2-2 span column, T3-2..4 resume ids, P4-1..5 output kit (+7 review fixes, quiet flag errors in agent/json). `make check` green. Sandbox: zh/z0/:filter only/span OK. |
| 3 | **Merged** @ 1e7c1ad: T5 search, T4-2/T4-3 uc (+::undone attach fix), T3-5..6 Resume intent + column + :rid, U6-1..3 vt10x REP/tabs + logo fixtures, K2 trusted-only skip_permissions, FF0 + flags board_filter/search/undo_complete/resume/summary_preview, S1–S3 summarizer, P2-1/2 trust core (+home identity/ancestor refusal), A1-2 doctor agy version, P3 H4–H16. `make check` green. |
| 4 | Lane A: P1-1..8 (wizard + skills, spawn_skills flag). Lane B: T2-3 ls -wide + timestamps flag, T3-7 rex resume, P4-6..13. Lane C: S4-1..9 overview. (building) |
