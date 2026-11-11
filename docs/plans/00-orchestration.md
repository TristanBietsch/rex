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
| U4 | cmd for thick colored window border (pantone, colorblind-safe, random default) | plan pending |
| U5 | fixed-width columns, responsive, mobile layout | plan pending |
| U6 | agy logo centered / not cut off in attach window | plan pending |
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
| 3 | T5-1..5 → T4-2 | T3-5..7 → T2-3 → T4-3 → A1-2 | S1-5,6 → S2-3,4 → S3-2..7 → S1-7, S2-5 |
| 4 | P1-1..8 | S4-1..9 | P3-4..11 |
| 5 | P2-1..6 | S5-1..6 | A1 capture (D2) → A1-4..7 |
| 6 | U1-1, U1-3..6, U1-8..12 (single builder) | — | — |
| 7 | U2 (YAML scope per D1) | — | — |

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
| 2 | Lane A: U1-7 → T1-1..8 → T2-2. Lane B: T3-2 → T3-3 → T3-4. Lane C: P4-1..5, then S1-1 → A1-3 → S1-2..4. |
