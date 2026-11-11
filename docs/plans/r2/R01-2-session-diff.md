# R01-2 Session diff / review page

## Summary
- A full-screen page (R04 frame) that shows what a session changed: a file list with `+/-` counts on the left, the unified diff of the selected file on the right. Range: the last turn (default, D3), or the whole session (first start capture → now).
- Opened with `d` on a card, `:diff [sel]`, or from S4's "ready for review" overview. CLI: `rex diff <sel> [--turn N | --all] [--stat] [-o text|json|patch]`.
- Data comes from R01-1 snapshots, so it needs `turn_snapshots` on. Behind `session_diff` (on).

## Current behavior
- Nothing in rex shows file changes. Users review in their editor or `git diff`, which mixes all agents' changes and their own.
- S4 classifies sessions as "ready for review" but offers nothing to review with.

## Design
- Daemon intent `Diff{session, from, to}` where `from`/`to` are `turn:<n>:start|end`, `first`, `now` (a fresh capture). It returns `DiffResult{files []FileDiff, truncated bool}`; `FileDiff{path, status A|M|D|R, add, del int, binary bool, hunks string}`.
  - Diff computed with the shadow store (`git diff --no-color --find-renames <a> <b>`), never the user's repo.
  - Caps: 2,000 files, 256 KiB of hunk text per file, 4 MiB total; beyond that `truncated` and the page says how to see the rest (`rex diff <sel> -o patch > file`).
- **Page layout** (two panes, like R04):
  ```
  ∴ REX › DIFF  fix-auth (claude · sonnet)      last turn · whole session
  ──────────────────────────────────────────────────────────────────────
  ▸ M internal/auth/token.go    +12 -3 │ @@ -40,7 +40,9 @@ func Check(…)
    A internal/auth/token_test.go +48  │ -   if exp < now {
    D docs/old.md                  -20 │ +   if !exp.After(now) {
  ──────────────────────────────────────────────────────────────────────
  esc back  tab pane  enter expand  e edit  t turn/session  [ ] turns  / filter
  ```
  - Syntax: diff colors from U1 tokens (`success` add, `danger` del); no language syntax highlighting (D1).
  - `enter` expands the diff pane full width; `e` opens the file in `$EDITOR` at the first hunk (D2).
  - Binary files: "binary, 12 KiB → 14 KiB".
- "Concurrent sessions" notice: if another session changed the same file in the same time window, the file row shows `⋈` and the page says the diff may include the other session's edits (snapshots are per folder, not per process).

## Feature flag
- `session_diff` (on). Effective only while `turn_snapshots` is on; otherwise `d`/`:diff` say "turn on turn_snapshots first". Off = keys unbound, `Diff` intent refused, `rex diff` answers "off".
- Off-is-inert test in R01-2-1.

## Plan
1. **R01-2-1 Diff intent.** Uses `snapshot.Changed` + hunks from R01-1-1. Tests: added/modified/deleted/renamed; binary detection; caps set `truncated`; `now` takes a capture without writing a turn ledger line; flag off → refused.
2. **R01-2-2 Page.** `diff_page.go` on the R04 frame: file list, hunk pane, turn stepping, filter. Goldens at 120×40 and 80×24, mobile stacks panes; empty diff shows "no file changes in this range".
3. **R01-2-3 Entry points.** `d` on a card, `:diff`, S4 overview "review" action, mouse click on file rows. Tests: `d` on a session with no snapshots explains why.
4. **R01-2-4 CLI.** `rex diff` with `--stat`, `-o patch` (raw unified diff, applies with `git apply`), `-o json`, P4 agent mode. Tests: `-o patch` round trip applies cleanly to the start tree.
5. **R01-2-5 Docs + CHANGELOG.** `docs/tui.md`, `docs/cli.md`.

## Review focus
- Huge generated file (lockfile) changed: capped, page stays responsive (hunks loaded lazily per selected file).
- File renamed and edited: shown once as `R` with the content diff.
- Diff contains secrets: shown locally only; `-o patch` writes to stdout, never to a shared location.
- Session in a folder without snapshots (flag turned on mid-session): range starts at the first capture; the page says "changes before <time> not recorded".
- Non-UTF-8 file content: shown with replacement characters, never breaks the terminal (sanitize control bytes).

## Decisions (answered 2026-11-11)

| # | Question | Answer |
|---|---|---|
| D1 | Syntax highlighting | None; add/del colors from U1 tokens only. |
| D2 | `enter` on a file | `enter` expands the diff full width; `e` opens `$EDITOR` at the first hunk. |
| D3 | Default range | Last turn; `t` switches to the whole session. |
