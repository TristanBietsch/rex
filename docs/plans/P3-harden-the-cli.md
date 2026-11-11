# P3 Harden the CLI

**Summary**
- An audit of `cmd/rex/main.go` and `internal/surface/cli/**` found real defects:
  - flags placed after a positional argument are swallowed, which breaks the `rex new` command that `rex setup` itself prints;
  - `-h` exits 5;
  - `rex setup` drops the tool, model and effort it asks for, and the interactive wizard resets the other settings;
  - `rex config set auto_trust_dirs` is not validated;
  - `rex wait -until bogus` hangs forever;
  - a possible nil dereference on stdin;
  - multi-byte characters are cut mid-character in `rex ls`.
- Below is a prioritized list (P0 first). Every item gets a test that fails before the fix.
- Several fixes are deliberate behavior changes. The e2e scripts mark them as `BUG` today; those lines get flipped.

**Current behavior (verified with `go run ./cmd/rex` under an isolated `HOME`)**
- All 28 call sites use `flag.ContinueOnError` and stop parsing at the first positional.
  - `rex new hello -tool claude` keeps tool `echo`, and the prompt becomes `hello -tool claude`. `e2e/testdata/script/session_new.txtar` documents this as "BUG … flip when fixed".
  - `rex wait p1 -until done` exits 5 (`session_io.txtar:14`).
  - `rex attach x -read-only` exits 5, although `docs/cli.md` shows exactly that order.
  - `setup.go:156` and `setup_wizard.go:1664` both print `rex new "your first task" --tool claude --model opus`, which hits the bug.
- `rex new -h` prints usage, then `rex: flag: help requested`, and exits 5.

**Prioritized hardening list**

| ID | Pri | Issue (file:line) | Fix | Test |
|---|---|---|---|---|
| H1 | P0 | `rex config set auto_trust_dirs` not validated (`config.go:113-127`; HANDOFF #5). Bad values are saved, then silently ignored by the daemon. | Add `Setting.Normalize func(string) (string, error)`. For `auto_trust_dirs`: `""` stays as is, otherwise `CheckAutoTrustDirs`. Applied in `Store.Set` for string input, so the CLI and TUI share it. `Load` is unchanged (a bad stored value stays visible; doctor reports it). | `settings`: `TestSetAutoTrustDirsValidates` (`/`, `~`, `rel`, `a/../b`, an entry containing `:`, a valid value tidied). `setup`: `TestConfigSet_AutoTrustRefused` (exit 5, file unchanged). Add to `config.txtar`. |
| H2 | P0 | Flags after positionals are ignored (every `fs.Parse`). | New `core.ParseArgs(fs, args) ([]string, error)`: accepts flags anywhere and stops at `--`. Applied at all 28 sites. For free-text commands (`new`, `reply`, `name`), an unknown `-x` after the text errors with the hint `use -- before text that starts with -`. Fixes the setup "next command" too. | `core`: `TestParseArgs` table (flags after positionals, `--`, a bare `-`, `-f=v`, bool flags, `C:\x` values, unknown flag after text). Per-command: `TestRunWait_FlagsAfterSelector`, `TestParseNewArgs_FlagsAfterPrompt`. Flip `session_new.txtar` and `session_io.txtar:14`. |
| H3 | P0 | `-h` exits 5 with "flag: help requested". | `ParseArgs` maps `flag.ErrHelp` to `core.ErrHelpShown`. In `main.go`, `run`'s caller prints nothing and `exitCodeFor` returns 0. | New `cmd/rex/main_test.go`: `TestRun_HelpFlagExitsZero` for every command. Add to `help_version.txtar`. |
| H4 | P0 | `rex setup` drops what it asks: `ApplyDefaultsNonInteractive` ignores `-tool/-model/-effort` (`setup_wizard.go:1604-1674`). The interactive `doWriteCmd` starts from `NewStore()` (lines 884-933), so on overwrite every other setting is reset to its default, and `default_spawn_*` is never written. | Load the existing store. Validate tool, model and effort against the registry (unknown → exit 5, listing the valid ids). Write `default_spawn_tool/model/effort`. The wizard's free-text model field is validated on write. | `TestApplyDefaults_WritesSpawnDefaults`, `_KeepsOtherKeys`, `_RejectsUnknownTool`; `TestSetupWizardWrite_KeepsMasterVolume`. |
| H5 | P0 | `rex new -cwd rel/dir` is sent as given; the daemon resolves it against its own cwd (`new.go:31-37`, `validate.go:25-39`). | Client: `filepath.Abs`, then it must exist and be a folder (exit 5). Daemon: `validateNewSession` requires `filepath.IsAbs(CWD)` (`badIntent`). | `TestParseNewArgs_RelativeCwdAbsolutized` / `_MissingCwd`; `server`: `TestValidateNewSession_RelativeCwd`. |
| H6 | P0 | `fi, _ := os.Stdin.Stat(); fi.Mode()` is a nil dereference when stdin is closed (`new.go:43`, `rm.go:34`). | `core.StdinIsTerminal()` built on `golang.org/x/term` (already a dependency), used everywhere including `prune.go:30`. Prompt reading goes through an injectable `io.Reader`. | `TestPromptFromStdin_ClosedStdin` (fake `*os.File` closed before the call); `TestStdinIsTerminal_Pipe`. |
| H7 | P1 | `rex rm` in a non-TTY deletes without confirmation, and an abort exits 1 (`rm.go:34-42`). `prune` requires `-yes`. | Same rule as prune: a non-TTY needs `-force` (alias `-yes`/`-y`), else exit 6. A declined prompt exits 6. | `TestRunRm_NonTTYNeedsForce`, `TestRunRm_DeclineExits6`. Update e2e scripts that run `rm` (add `-force`). |
| H8 | P1 | `rex wait -until bogus` waits forever; a negative `-timeout` means no timeout; the timeout is detected by matching error text (`wait.go:18-56`). | Validate `-until` and `-timeout >= 0` (exit 5). Use `errors.Is(err, os.ErrDeadlineExceeded)`. Docs fix: a removed session exits **2**, not 3 (`docs/cli.md`, "rex done" section). | `TestRunWait_BadUntil`, `_NegativeTimeout`; add to `exit_codes.txtar`. |
| H9 | P1 | Daemon not running: the raw `dial unix …: no such file` message. `ResolveSelector`'s `Hello` error exits 1 (`selector.go:18-22`). `render` returns raw errors (`render.go:21-…`). | `SocketFlag.Dial` adds "rex-daemon is not running (no socket at X); start it with `rex daemon start`" on ENOENT/ECONNREFUSED. A `Hello` failure becomes exit 4. `render` uses `socket.Dial`. | `TestDial_NoDaemonMessage`, `TestResolveSelector_HelloFailExit4`, `TestRunRender_NoDaemonExit4`; `no_daemon.txtar` checks stderr. |
| H10 | P1 | Selector ambiguity: an exact slug silently beats a hex prefix that matches a different session (`selector.go:54-74`; prompt-derived slugs like `feed`, `cafe`, `added` are valid hex). Uppercase short ids never match. The help text advertises `@needs/@working/@done`, which do not exist (`help.go:128`). | Slug and short-id prefix on different sessions → exit 3 listing both. Lowercase the selector for hex matching. Remove the `@` aliases from help (or implement them; see the open questions). | `TestResolve_SlugVsHexPrefixAmbiguous`, `TestResolve_UppercaseShortID`, `TestHelp_NoAtAliases`; the fuzz target is unchanged. |
| H11 | P1 | `core.Truncate` slices bytes (`output.go:132-141`): `claude-sonnet-4-6 · medium` is cut inside `·`, producing invalid UTF-8, and `%-20s` pads by bytes, so columns drift. | Width-aware truncate and pad (`charmbracelet/x/ansi`, already a dependency). | `TestTruncate_MultiByte`, `TestWriteSessionsTable_ValidUTF8AndAligned`. |
| H12 | P2 | Unknown command exits 1 (`main.go:94`). | Exit 5 with a "did you mean" suggestion (edit distance ≤ 2). Docs updated. | `TestRun_UnknownCommandSuggests`; add to `exit_codes.txtar`. |
| H13 | P2 | `rex reply <sel>` with no text and a TTY stdin blocks on `ReadAll` (`reply.go:28-35`). `rex log -n -5` is accepted. `docs/cli.md` shows `-bytes N`, but it is a bool. | A TTY with no text → exit 5. `-n < 0` → exit 5. Fix the docs. | `TestRunReply_TTYNoText`, `TestRunLog_NegativeN`. |
| H14 | P2 | `setup -y` uses a hand-scan alias (`setup.go:24-26, 45-51`). No-op flags `ls -short`, `ls -show-archived`, `new -no-attach`. | `fs.BoolVar(&yes, "y", …)` as prune does. Usage text for the no-op flags says "(deprecated, no effect)"; they stay accepted. | `TestRunSetup_YAlias`, `TestUsage_DeprecatedFlagsMarked`. |
| H15 | P2 | `rex new` gives up after a fixed 5 s with a raw deadline error (`new.go:93-99`). | Message: "daemon did not confirm the spawn within 5s; check `rex ls`", exit 1. Optional `-wait` duration. | `TestRunNew_SpawnConfirmTimeoutMessage` (fake daemon that never sends `SessionAdded`). |
| H16 | P2 | Windows: the `ModeCharDevice` check treats `< NUL` as a TTY; `-cwd \foo` is not absolute; the list separator `;` in messages. | Covered by H6 (`x/term`), H5 (`Abs`) and H1 (the error names `filepath.ListSeparator`). Add Windows-specific test cases guarded by `runtime.GOOS`. | `ParseArgs` cases with `C:\`; `TestNewCwd_WindowsDriveRelative` (Windows only); `make lint-windows`. |

**Design notes**
- `core.ParseArgs` is the single parser. New commands from P1, P2 and T* must use it.
- The exit-code mapping stays in `core` (`version.go:21-44`); `ErrHelpShown` is added there.
- `Setting.Normalize` is generic; future validated string settings reuse it.

**Files to change**
- `cmd/rex/main.go` (`main`: no output for `ErrHelpShown`; `exitCodeFor`; the `run` default branch). New `cmd/rex/main_test.go`.
- `internal/surface/cli/core/{socket.go,selector.go,output.go,help.go,version.go}`, new `core/args.go`, `core/term.go`.
- Every `internal/surface/cli/**` file that calls `fs.Parse` (28 sites).
- `session/{new.go,rm.go,wait.go,reply.go,prune.go}`, `inspect/{render.go,log.go}`, `setup/{config.go,setup.go}`.
- `internal/catalog/settings/{types.go,store.go,registry.go}`.
- `internal/surface/tui/setup_wizard.go` (`doWriteCmd`, `ApplyDefaultsNonInteractive`).
- `internal/daemon/server/validate.go` (`validateNewSession`).
- e2e: `session_new.txtar`, `session_io.txtar`, `exit_codes.txtar`, `config.txtar`, `help_version.txtar`, `no_daemon.txtar`.

**Docs/CHANGELOG**
- `docs/cli.md`: selector rule, exit codes (unknown command 5, removed session 2, `rm` non-TTY 6), flags may follow arguments and `--`, `-bytes` is a bool, the BUGS section shrinks.
- `docs/settings.md` (config set validates `auto_trust_dirs`), `docs/quickstart.md` (example now works as written).
- CHANGELOG **Fixed** and **Changed**, with the breaking changes called out: `rm` in scripts needs `-force`; unknown command now exits 5; text starting with `-` needs `--`.
- `HANDOFF.md`: item 5 is done.

**Conflict map**
- P1/P2: both add flags to `new.go` and cases to `main.go`. P3's H2, H4 and H5 must land first; they then use `core.ParseArgs`.
- U3: shares the H4 registry validation for spawn defaults (it reuses U3's `resolveSpawnDefault` logic if that has landed; otherwise H4 brings a small `registry`-based validator in `setup`).
- T1-T5: any new `rex ls` flags (filter, search, timestamps) must use `ParseArgs`. `output.go` table changes from T (timestamps) should build on the H11 width-aware helpers. Land H11 early.
- S-tasks: none. U1/U2: none (CLI only).
- `main.go` regions: P3 owns `main()`, `exitCodeFor` and the `default:` branch of `run`. Others only add `case` lines.

**Security notes**
- H1 closes a gap where the CLI could store a value that `rex setup` refuses (root, home, `..`). The daemon already ignored such values, but the user was misled.
- H5 removes a dependence on the daemon's cwd.
- H7 removes an unprompted destructive path.
- H10 prevents `rm` or `complete` acting on the wrong session.
- None of these touch the socket, PTY input or transcripts.

**Open questions for user**
1. Should `rex rm` in scripts require `-force` (H7)? **Recommended: yes**, matching prune.
2. Should unknown commands exit 5 instead of 1 (H12)? **Recommended: yes.**
3. `@needs/@working/@done` selectors: remove them from help, or implement them (exit 3 when several match)? **Recommended: remove now**, implement later if wanted.

**Ordered steps (each one commit, `make check` green)**
1. `fix(cli): width-aware truncation in session tables` (H11).
2. `feat(cli): parse flags anywhere; -h exits 0` (H2, H3, plus the e2e flips).
3. `fix(config): validate auto_trust_dirs on set` (H1).
4. `fix(setup): write spawn defaults and keep existing settings` (H4).
5. `fix(new): absolute, existing -cwd; daemon requires absolute cwd` (H5).
6. `fix(cli): safe terminal detection; rm needs -force without a terminal` (H6, H7, H16).
7. `fix(wait): validate -until and -timeout; detect timeouts by error` (H8).
8. `fix(cli): clear daemon-not-running errors and exit 4 everywhere` (H9).
9. `fix(selector): report slug/short-id collisions; drop @ aliases from help` (H10).
10. `fix(cli): unknown command exit 5 with suggestion; reply/log arg checks; -y alias; deprecated flags; spawn confirm timeout message` (H12-H15).
11. `docs(cli): exit codes, selectors, flag order; changelog; handoff item 5`.

Raise the coverage floors in `tools/coverage/baseline.txt` for `cli/core` (53), `cli/session` (29) and `cli/setup` (62) as coverage rises.

**Size**: L (many small commits)

### Critical Files for Implementation
- /Users/tristan/Documents/personal/dev/rex/internal/catalog/registry/loader.go
- /Users/tristan/Documents/personal/dev/rex/internal/daemon/server/spawn.go
- /Users/tristan/Documents/personal/dev/rex/internal/surface/tui/wizard.go
- /Users/tristan/Documents/personal/dev/rex/internal/surface/cli/session/new.go
- /Users/tristan/Documents/personal/dev/rex/internal/catalog/settings/trust_dirs.go
