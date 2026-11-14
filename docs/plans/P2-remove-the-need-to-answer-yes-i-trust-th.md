# P2 Remove the need to answer "yes, I trust this folder"

**Summary**
- `auto_trust_dirs` covers only folders listed ahead of time, and the TUI cannot add to that list. Sessions in new folders (fresh clones, worktrees, a TUI started from `~`) therefore still stop at Claude's "Yes, I trust this folder" dialog, or agy's.
- Fix: one keypress in the spawn flow, either "trust this session" or "always trust this project's parent folder" (an appended, validated `auto_trust_dirs` entry). Also a `rex trust` / `:trust` command, `rex new -trust`, and feedback when a dialog shows up but was not answered.
- It keeps every check from PR #14: no `..`, no root, no home, no relative path, symlinks resolved.

**Current behavior (the gaps)**
- **G1.** `auto_trust_dirs` defaults to `""` (`settings/registry.go`, the `auto_trust_dirs` entry). It is set only by `rex setup` (`setup_wizard.go:653-740`) or `rex config set`. The settings page cannot edit it: a `TypeString` without options does nothing (`settings.go:170-173`, `settingAdjustable` at lines 463-478).
- **G2.** The spawn folder is the TUI process's own cwd (`wizard.go:260`, `update.go:577`). When `rex` is started from `~`, the home folder is refused by `TrustDir` (`trust_dirs.go:68-73`), so those sessions are never auto-trusted.
- **G3.** `rex config set auto_trust_dirs <bad>` is saved without validation (`cli/setup/config.go:113-127`). The daemon then ignores it silently (`boot/trust.go`, `server/trust.go:73-88`). This is HANDOFF item 5, fixed in P3-H1.
- **G4.** The answerer acts only when armed (`server/trust.go:17-38`). When the dialog appears in a folder that is not listed, or Claude's wording drifts away from `shown`, nothing tells the user why. The session simply reads `needs_input` after 15 s.
- **G5.** agy: `trust_args` apply only to listed folders (`server/trust.go:44-66`). A1 owns the agy entry.
- **G6.** Claude remembers trust per folder in its own config, so each new folder (worktree, clone) prompts once. Unverified: whether a trusted parent covers its subfolders.
- The setup trust copy names only Claude (`setup_wizard.go:1217`). It should also name Antigravity.

**Design**
- **D0 (prerequisite, P3-H1).** `Store.Set("auto_trust_dirs", v)` validates through `CheckAutoTrustDirs`, with `""` allowed.
- **D1. Per-session trust.**
  - `NewSession` gains `TrustFolder bool \`json:"trust_folder,omitempty"\``.
  - In `handleNewSession`: when set, `settings.TrustDir(p.CWD)` must pass (relative, `..`, root and home are refused → `badIntent`). Then the answerer and the `trust_args` are armed as if the folder were listed.
  - Refactor: `trustAnswerFor` and `trustArgsFor` take a resolved `trustDecision{dir string, ok bool, why string}` computed once by `srv.trustDecision(cwd, explicit bool)`. `underAutoTrustDir` itself does not change.
  - `slog` records `why=auto_trust_dirs|session`.
  - Nothing is persisted by rex. Note for the docs: when the dialog is answered for Claude, Claude records it in its own config, exactly as if the user had pressed yes.
- **D2. Shared helpers.**
  - Move `suggestTrustDir` (`setup_wizard.go:747-774`) to `settings.SuggestTrustDir(cwd)`, extended so that a non-git folder inside home (not home itself) suggests itself.
  - Add `settings.AppendTrustDir(cur, dir string) (string, bool, error)`: no-op if the folder is already covered; otherwise join and validate the whole value with `CheckAutoTrustDirs`.
  - Add `settings.RemoveTrustDir`.
- **D3. Wizard and quick-spawn UX.**
  - On the describe step, when the cwd is not covered, show one line under the input: `folder not trusted · ctrl+t this session · ctrl+a always trust ~/dev`.
  - `ctrl+t` sets `TrustFolder`.
  - `ctrl+a` marks the suggestion. On launch it appends it to the config (saved through the store), then spawns with `TrustFolder` set.
  - When the cwd is covered, show a dim line `trusted (auto_trust_dirs)`. When the cwd is home or root, show `home folder: start rex in a project folder to trust it` (G2).
  - New `:trust [path]` command (`command.go`): appends the given path or the suggestion and shows the result or the error in `m.Err`.
- **D4. CLI.**
  - `rex new -trust` sends `TrustFolder`.
  - New `rex trust [dir]`: appends (default: the suggestion for `$PWD`) and prints the new value. `rex trust -ls` lists. `rex trust -rm dir` removes. Everything is validated, and refusals exit 5.
- **D5. Feedback for G4.**
  - `SupervisorConfig.TrustDetect *regexp.Regexp`: the tool's `shown`, passed even when not armed.
  - When it matches within the first 60 s and no answerer is armed, the supervisor logs once: `pty: trust dialog shown; folder not trusted (rex trust, or ctrl+t in the wizard)`.
  - `rex doctor` gains `checkAutoTrustDirs` (warns when the stored value would be ignored), appended after A1's row.
  - A board-level "accept the trust dialog now" key is deferred (see the open questions).
- **D6.** Setup trust copy (`renderTrust`, `setup_wizard.go:1217`): "Claude Code and Antigravity ask …".

**Files to change**
- `internal/catalog/settings/trust_dirs.go` (`SuggestTrustDir`, `AppendTrustDir`, `RemoveTrustDir`).
- `internal/surface/tui/setup_wizard.go` (`suggestTrustDir` delegates to the moved function; `renderTrust` copy).
- `internal/wire/protocol/intents.go` (`NewSession.TrustFolder`).
- `internal/daemon/server/trust.go` (`trustDecision`, and the signatures of `trustAnswerFor`/`trustArgsFor`), `spawn.go` (the two call sites at lines 60 and 151).
- `internal/daemon/pty/supervisor.go` (`SupervisorConfig.TrustDetect`, the trust branch around lines 477-490), `pty/trust.go` (`unansweredDetector`).
- `internal/surface/tui/wizard.go` (`WizardState.TrustSession`/`TrustAlways`, `updateWizardDescribeStep`, the render case), `command.go` (`case "trust"`).
- `internal/surface/cli/session/new.go` (`-trust`), new `internal/surface/cli/setup/trust.go` (`RunTrust`), `cmd/rex/main.go` (`case "trust"`).
- `internal/surface/cli/setup/doctor.go` (new `checkAutoTrustDirs`, appended).

**Test plan (test-first)**
- `settings`:
  - `TestAppendTrustDir`: dedupe, already covered by an entry is a no-op, `..`/root/home/relative refused, order kept, `;` on Windows;
  - `TestRemoveTrustDir`;
  - `TestSuggestTrustDir` (moved), plus non-git-folder and home cases.
- `server` (`trust_test.go`):
  - `TestTrustDecision_ExplicitSession`: valid folder armed without `auto_trust_dirs`; home, root, `..`, relative and a symlink escape refused;
  - `TestNewSession_TrustFolderHomeIsBadIntent`;
  - `TestTrustArgsFor_ExplicitExpandsCwdList` (agy-style args).
- `pty`: `TestUnansweredTrustDialogLoggedOnce` (fake screen, `slog` capture); window expiry.
- `tui`:
  - `TestWizardDescribe_CtrlTSendsTrustFolder`;
  - `TestWizardDescribe_CtrlAAppendsAndTrusts` (temp `HOME` and `USERPROFILE`);
  - `TestWizardDescribe_CoveredFolderShowsTrusted`;
  - `TestCommandTrust_InvalidPathShowsError`.
- `cli`: `TestRunTrust_AddListRemove`, `TestRunTrust_RefusesDotDot` (exit 5), `TestParseNewArgs_Trust`. e2e txtar `trust.txtar` (config file contents checked).

**Docs/CHANGELOG**
- `docs/settings.md` (Auto-trust: per-session trust, `rex trust`, `:trust`, ctrl+t/ctrl+a; the note that Claude remembers an accepted dialog).
- `docs/cli.md` (`rex trust`, `rex new -trust`), `docs/tui.md`, `docs/protocol.md`.
- `SECURITY.md`, out-of-scope paragraph: "folders you list **or trust for one session**".
- CHANGELOG **Added**.

**Conflict map**
- P1: both touch the wizard describe step and `NewSession`. P1's refactor lands first; P2 adds separate fields and functions.
- P3: H1 (config validation) is a hard prerequisite. `new.go` flag parsing lands first (H2/H4). `main.go`: P2 adds `case "trust"`.
- A1: owns the agy `trust_args` value. P2 only changes when `trustArgsFor` fires (now also on an explicit session trust). A1's agy trust tests stay valid. A1 appends `checkAgyVersion` in `runAllChecks`; P2 appends `checkAutoTrustDirs` after it, with no restructure.
- U1/U2: will restyle the wizard line and may make `auto_trust_dirs` editable on the settings page. P2 does not touch `settings.go`.
- T-tasks: none.

**Security notes**
- A per-session trust adds no new privilege. A socket client can already type `\e[B\r` into the PTY. The request is still limited to the session's own folder and refused for root, home, relative paths and paths with `..` (CanonicalDir/TrustDir, the PR #14 rules).
- Appended entries go through the same `CheckAutoTrustDirs` as setup and the daemon, so they cannot drift.
- Suggestions must lie inside the canonical home (the `suggestTrustDir` logic is kept).
- Pasted control characters are stripped from typed paths, as `keyTrustFolder` does today.
- rex never writes `~/.claude.json` or `~/.gemini`. Default behavior is unchanged: nothing is trusted unless the user acts.

**Open questions for user**
1. Should `ctrl+t` (trust this session) be on by default for git checkouts inside home? **Recommended: no.** Keep it an explicit keypress. SECURITY.md treats "trusting a folder you did not list" as in scope.
2. Command name: `rex trust` or `rex config trust`. **Recommended: `rex trust`.**
3. Add a board key (`T`) that answers a trust dialog already on screen for a live session (a new `AcceptTrust` intent)? **Recommended: defer** until D3/D4 have been used for a while.

**Ordered steps (each one commit, `make check` green)**
0. (P3-H1 merged.)
1. `refactor(settings): shared trust-folder suggestion, append and remove`.
2. `feat(protocol,daemon): trust the session folder on request`: `TrustFolder` and `trustDecision`.
3. `feat(cli): rex trust and rex new -trust`.
4. `feat(tui): trust this session or this project folder from the wizard; :trust`.
5. `feat(daemon,doctor): log an unanswered trust dialog; check auto_trust_dirs`.
6. `docs: trust workflow, SECURITY scope, setup copy names Antigravity`.

**Size**: M
