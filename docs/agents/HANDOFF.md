# Handoff: start here

For the next coding agent on rex. Read this first, then follow the
onboarding order. Last updated 2026-11-10, after PRs #6–#17 and #19.

## 1. Onboarding order

1. [../../README.md](../../README.md): what rex is, install, first session.
2. [../../CONTRIBUTING.md](../../CONTRIBUTING.md): make targets, test tiers,
   commit and PR checklist.
3. [README.md](README.md): module map and "where to start" per task.
4. [AGENTS.md](AGENTS.md): authority (Go source wins), safe vs careful edits.
5. [../STRUCTURE.md](../STRUCTURE.md): where new code goes.
6. [../../CHANGELOG.md](../../CHANGELOG.md) `[Unreleased]`: what changed
   recently, in user terms.
7. The module doc for the area you touch (`modules/*.md`), then the source.

## 2. The gate

```sh
make check   # vet, race tests, lint (host + windows), coverage floors, govulncheck
```

Run it before every PR. If several worktrees lint at once, golangci-lint can
fail on its lock. Run the linter by hand with `--allow-parallel-runners`:

```sh
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run --allow-parallel-runners ./...
go run -exec 'env GOOS=windows' github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run --allow-parallel-runners ./...
```

Keep the version in step with `GOLANGCI_LINT_PKG` in the `Makefile`.

## 3. Conventions that bit us

- **Conventional Commits**: `type(scope): summary` (see CONTRIBUTING.md).
- **Signed commits**: signing goes through 1Password. If signing fails,
  retry once. Never bypass it (no `--no-gpg-sign`, no config change).
- **Independent review**: every PR gets a review from a separate agent
  before merge. Do not review your own PR and merge it.
- **CHANGELOG**: user-visible changes get an entry under `[Unreleased]`.
- **Coverage floors**: when coverage rises, raise the package's floor in
  `tools/coverage/baseline.txt` to the measured value (`make cover`).
- **Windows CI catches test-only path issues.** macOS/Linux pass, Windows
  fails. Watch for:
  - folder or file names with `"`, `\` or trailing spaces (illegal on
    Windows; skip the case there or pick a legal name);
  - `HOME` vs `USERPROFILE` (set both, or resolve through `internal/paths`);
  - `t.TempDir()` cleanup fails while a child process still runs in it;
    stop the child before the test ends.
- **The repo is public.** Never paste live session output (`rex render`,
  `rex ls`, transcripts, screen dumps) into PRs, issues or commits. Use
  synthetic examples. No real names, emails or personal paths.

## 4. What shipped this round

- #6 vt10x: ignore CSI sequences with `<` `=` `>` markers (attach freeze).
- #7 attach popup render perf; vt10x stops allocating per byte.
- #8 marking a session done plays the done sound.
- #9 `rex prune` and `:prune` clear the Completed column.
- #10 `auto_trust_dirs`: daemon answers Claude Code's folder-trust dialog.
- #11 simpler board: `v` for detail, `tab` next needs-you, Done column folds.
- #12 subagent count column for Claude sessions (`SubagentStart`/`Stop` hooks).
- #13 Windows CI test fixes.
- #14 `rex setup` auto-trust step; refuses `..` paths (security fix).
- #15 Antigravity CLI (`agy`) replaces Gemini; adds `trust_args`, `busy_regex`.
- #16 attach repaint: show a running agent's screen right away (black
  window fix).
- #17 require Go 1.26.11 (GO-2026-6859).
- #19 TUI screensaver: centered `∴ rex` with live counts after idle; keeps
  the macOS display awake (`caffeinate`).

## 5. Open items: where we left off

1. #16 (attach repaint) and #19 (screensaver) are merged. Check
   `gh pr list --state open` for anything newer.
2. Antigravity hook-based state needs the user's OK to write
   `~/.gemini/config/hooks.json`. Not done. Today state comes from the
   footer (`busy_regex`). Ask before touching that file.
3. #16 follow-ups:
   - the repaint can snapshot the screen in the middle of an escape sequence;
   - bracketed paste mode (`?2004`) is not tracked by vt10x.
4. #10 (`auto_trust_dirs`) and #12 (subagent count) were not tried against
   a live Claude run with background subagents.
5. `rex config set auto_trust_dirs` does not validate its value. `rex setup`
   does; the daemon ignores bad entries. Route it through the same checks.
6. More unchecked TODO items live in the user's local, gitignored `TODO.md`
   (not in the repo). Ask the user for it.

## 6. Doc debt noticed

- `docs/agents/` is a map and can lag the code; Go source wins.
- `IntentOpenSession` is still a protocol constant with no server handler
  (`bad_intent`); documented as such in `docs/protocol.md`.
- `docs/agents/modules/*.md` were not re-checked line by line for
  PRs #6–#17. Check the module doc against source before relying on it.
