# Medium hardening (M1–M7) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the medium findings of the security review: allowlisted agent and daemon environment, prompt by paste, attribution logging, local-only summarizer, escape filtering, complete uninstall, and tags only from master.

**Architecture:** Seven independent parts, each on its own branch from `master` (`harden/m<N>-<slug>`), each green on the full gate and merged (fast-forward) before the next starts. New code goes into small packages (`internal/daemon/childenv`, `internal/termfilter`) or next to the code it hardens. The wire protocol gains one optional field (`SessionSummary.PromptUndelivered`).

**Tech Stack:** Go 1.26.11, `golang.org/x/sys/unix`, testscript e2e, golangci-lint v2.14.0.

**Spec:** [medium-hardening.md](medium-hardening.md). Read the part's section before its tasks.

## Global Constraints

- Go 1.26.11 floor; no cgo in new code; no new third-party dependency.
- Windows must compile and lint: `GOOS=windows go vet ./...`, `make lint-windows`.
- Run every test through a sandbox wrapper `$SBX` that points HOME, XDG_* and REX_LOG_DIR at a fresh temp dir and strips `~/.local/bin` from PATH: `$SBX go test -count=1 ./pkg -run Name`. Never run repo code against the real `~/.config/rex`, `~/.local/share/rex`, `~/.cache/rex` or a running daemon.
- Behavior change: failing test first, watched failing for the right reason.
- Coverage floors in `tools/coverage/baseline.txt` never go down; raise a floor when coverage rises.
- Doc comment on every exported symbol added or changed.
- Never `append` onto a slice that came from the registry or settings; build new slices (`slices.Concat`, `slices.Clone`). Registry data is shared across goroutines.
- Conventional commit subjects; each message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`; commits are signed (retry a transient signing failure) and use the repository's own git identity.
- A `CHANGELOG.md` entry under `## [Unreleased]` for each user-visible change (M1 is **Changed** and breaking; the rest **Security**).
- Public repository: text is about rex only; no names of people, organizations or private conversations in code, docs, commits or branch names.
- Gate per part: `GOFLAGS=-count=1 $SBX make check` exits 0. `internal/daemon/boot` has a known `signal: hangup` flake; re-run that package before treating it as a failure.

## Review Focus

- **M1, Windows:** a child given only the allowlist must still start (`SystemRoot`, `COMSPEC`, `PATHEXT` missing breaks every program). Pinned by `TestBuild_WindowsBaseKeepsSystemNames` (Task E1) and the Windows CI job.
- **M1, provider variables:** Claude through Bedrock or Vertex, or on Windows with `CLAUDE_CODE_GIT_BASH_PATH`, must keep working in both modes. Pinned by `TestBuild_KeepsClaudeProviderVars` (Task E1) and `TestBuiltinEnvPass` (Task E2).
- **M2, the trust dialog is on screen when the paste would fire, or the user answers it late:** never type into it, and still deliver the prompt once it closes. Pinned by `TestSupervisor_PromptNeverTypedIntoTrustDialog` and `TestSupervisor_PromptDeadlinePausedWhileDialogShown` (Task F2).
- **M5, text that is not an escape:** a raw 0x9E byte (Windows-1252 `ž`) or an unterminated OSC must not swallow later output. Pinned by `TestFilter_RawC1ByteIsNotAString`, `TestFilter_EscEndsString`, `TestFilter_UnterminatedStringIsBounded` (Task B1).
- **M7, a workspace `.rex` replaced by a symlink**, or a workspace root that no longer exists: skip it, say so, delete nothing outside. Pinned by `TestRunUninstall_AllSkipsSymlinkedWorkspaceRex` (Task C1).

---

## Part A — M4: local-only summarizer (branch `harden/m4-summarizer-endpoint`)

### Task A1: Parse and check the Ollama endpoint

**Files:** Modify `internal/daemon/boot/ollama.go:81-90` (replace `OllamaBaseURL`); Test `internal/daemon/boot/ollama_endpoint_test.go` (new).

**Interfaces:** Produces `func OllamaEndpoint(raw string, allowRemote bool) (string, error)`; `var ErrRemoteSummarizer`; `const ollamaDefaultPort = "11434"`.

- [ ] **Step 1: Write the failing test** `TestOllamaEndpoint`, table-driven:

| raw | allowRemote | want | wantErr |
|---|---|---|---|
| `""` | false | `http://127.0.0.1:11434` | – |
| `0.0.0.0` | false | `http://127.0.0.1:11434` | – |
| `0.0.0.0:11434` | false | `http://127.0.0.1:11434` | – |
| `:11434` | false | `http://127.0.0.1:11434` | – |
| `::` | false | `http://127.0.0.1:11434` | – |
| `::1` | false | `http://[::1]:11434` | – |
| `[::1]:9999` | false | `http://[::1]:9999` | – |
| `127.0.0.1` | false | `http://127.0.0.1:11434` | – |
| `localhost:9999` | false | `http://localhost:9999` | – |
| `http://127.0.0.1:11434/` | false | `http://127.0.0.1:11434` | – |
| `https://gpu.example:11434` | false | – | `ErrRemoteSummarizer` |
| `https://gpu.example:11434` | true | `https://gpu.example:11434` | – |
| `http://gpu.example` | true | – | error containing `https` |
| `httpx://127.0.0.1` | false | – | error containing `scheme` |
| `http://u:p@127.0.0.1` | false | – | error containing `credentials` |

- [ ] **Step 2:** `$SBX go test -count=1 ./internal/daemon/boot -run TestOllamaEndpoint` → FAIL (undefined).
- [ ] **Step 3:** Order: `net.ParseIP(raw)` first (bare IPv4/IPv6); then a value with `://` goes through `url.Parse`; otherwise `net.SplitHostPort` (adding the default port when it fails with "missing port"). Empty or unspecified host → `127.0.0.1`. Loopback = `IP.IsLoopback()` or `host == "localhost"`.
- [ ] **Step 4:** PASS.
- [ ] **Step 5:** Commit `fix(summarizer): parse OLLAMA_HOST strictly and refuse remote hosts by default`.

### Task A2: No redirects, capped replies

**Files:** Modify `internal/features/summarizer/client.go:56-75, 159, 186`; Test `client_test.go`.

**Interfaces:** Produces `const maxReplyBytes = 1 << 20`.

- [ ] **Step 1:** `TestClient_RefusesRedirect` (a 307 to a second httptest server: `Complete` errors and the second server's hit count is 0); `TestClient_CapsReplySize` (2 MiB reply: `Complete` and `Tags` error).
- [ ] **Step 2:** FAIL. **Step 3:** `CheckRedirect` returns an error; decode from `io.LimitReader(resp.Body, maxReplyBytes+1)` and fail past `maxReplyBytes`. **Step 4:** PASS with the existing client tests. **Step 5:** Commit `fix(summarizer): follow no redirects and cap reply size`.

### Task A3: Setting, wiring, doctor row, docs

**Files:** Modify `internal/catalog/settings/registry.go` (near `summary_enabled`, :396), `settings/trust_ids.go:14`, `cmd/rex-daemon/main.go:210-212`, `boot/ollama.go:22-29` (`ProbeOllamaHealth`), `internal/surface/cli/setup/doctor.go:103-122`; Create `setup/doctor_summarizer.go`; Tests: trust-IDs test, `cmd/rex-daemon/overview_test.go` (must stay green: it uses a loopback httptest URL), `setup/doctor_summarizer_test.go`; Docs `docs/settings.md`, `SECURITY.md`, `CHANGELOG.md`.

**Interfaces:** Consumes `OllamaEndpoint`, `ErrRemoteSummarizer`. Produces setting `summary_allow_remote` (TypeBool, default false, `ApplyDaemonRestart`, in `TrustSettingIDs`); `func checkSummarizerEndpoint(getenv func(string) string, allowRemote bool) checkResult`.

- [ ] **Step 1:** `TestTrustSettingIDs_IncludeSummaryAllowRemote`; `TestCheckSummarizerEndpoint` (unset → Pass "local http://127.0.0.1:11434"; `https://gpu.example` with false → Fail naming `summary_allow_remote`; with true → Warn "remote").
- [ ] **Step 2:** FAIL. **Step 3:** On endpoint error, log at Warn, leave the summarizer off, publish a `SummarizerHealth` with the reason; `ProbeOllamaHealth` uses the resolved URL. **Step 4:** PASS (`./internal/catalog/settings ./internal/surface/cli/setup ./cmd/rex-daemon ./internal/daemon/boot`). **Step 5:** Commit `feat(summarizer): summary_allow_remote and a doctor row for the endpoint`; gate; merge.

---

## Part B — M5: escape filtering (branch `harden/m5-escape-filter`)

### Task B1: `termfilter` package

**Files:** Create `internal/termfilter/filter.go`, `filter_test.go`, `filter_fuzz_test.go`.

**Interfaces:** Produces `func NewWriter(w io.Writer) *Writer`; `func (f *Writer) Write(p []byte) (int, error)` (returns `len(p)` on success); `func (f *Writer) Flush() error` (emits a pending partial UTF-8 rune as U+FFFD; drops a pending escape); `const maxStringBytes = 64 << 10`.

- [ ] **Step 1:** Tests:
  - `TestFilter_Keeps`: `"a\tb\r\n"`, `"\x1b[1;31mred\x1b[0m"`, `"\x1b[2J\x1b[H"`, `"\x1b7x\x1b8"`, `"\x1b(B"`, `"é漢"` unchanged.
  - `TestFilter_Drops`: `"A"+seq+"B"` → `"AB"` for `"\x1b]52;c;aGk=\x07"`, `"\x1b]8;;http://x\x1b\\"`, `"\x1b]0;title\x07"`, `"\x1bPq#0\x1b\\"`, `"\x1b_apc\x1b\\"`, `"\x1b^pm\x1b\\"`, `"\x1bXsos\x1b\\"`, `"\u009d52;c;aGk=\u009c"`, `"\u202e"`, `"\u2066"`, `"\x07"`, `"\u0085"`.
  - `TestFilter_RawC1ByteIsNotAString`: `"A\x9d52;c;aGk=\x9cB"` → `"A52;c;aGk=B"`; `"caf\x9e text"` → `"caf text"`.
  - `TestFilter_EscEndsString`: `"\x1b]52;c;xx" + "\x1b[31mTAIL"` → `"\x1b[31mTAIL"`; `"\x1b]0;t\x18X"` (CAN) → `"X"`.
  - `TestFilter_UnterminatedStringIsBounded`: `"\x1b]" + strings.Repeat("a", maxStringBytes) + "bbbb"` → `"bbbb"` (the byte that would exceed the cap is the first one treated as text).
  - `TestFilter_EverySplitPoint`: one mixed input written in two chunks split at every byte gives the same output as one write.
- [ ] **Step 2:** FAIL (package missing).
- [ ] **Step 3:** Byte-level state machine (ground, ESC, CSI, string body, UTF-8 decode); a CSI passes once its final byte arrives; C1 runes are recognized only after UTF-8 decoding; raw 0x80–0x9F outside valid UTF-8 is a single dropped byte. Model for chunk handling: `ansiStripper` in `internal/surface/cli/inspect/ansi.go:47`.
- [ ] **Step 4:** PASS. Add `FuzzFilter`: no panic; output never contains `"\x1b]"`, `"\x1bP"`, `"\u009d"`, `"\u202e"`, or a raw byte 0x80–0x9F outside valid UTF-8. Run `$SBX go test -run=^$ -fuzz=FuzzFilter -fuzztime=30s ./internal/termfilter`.
- [ ] **Step 5:** Commit `feat(termfilter): drop OSC, DCS and C1 controls while keeping colors and cursor moves`.

### Task B2: `rex attach` filters output and resets more on detach

**Files:** Modify `internal/surface/cli/session/attach.go:31-32` (`exitAlt`), `:181-209` (`streamAttach`); Test `attach_filter_test.go` (new; reuse the fake daemon from `attach_repaint_test.go`).

**Interfaces:** Consumes `termfilter.NewWriter`.

- [ ] **Step 1:** `TestStreamAttach_FiltersReplayAndLive` (replay and live output with `"\x1b]52;c;aGk=\x07"` and `"\x1b[31mok"`: stdout has `"\x1b[31mok"`, not `"]52;"`); `TestExitAlt_ResetsKeyboardAndCursor` (`exitAlt` contains `"\x1b[0 q"`, `"\x1b[?2026l"`, `"\x1b[>4;0m"`, `"\x1b[<u"`).
- [ ] **Step 2:** FAIL. **Step 3:** One filter per attach, `Flush` before `exitAlt`. **Step 4:** PASS. **Step 5:** Commit `fix(attach): filter agent escapes and reset keyboard modes on detach`.

### Task B3: TUI popup drops C1 and bidi runes

**Files:** Modify `internal/surface/tui/attach_render.go:119-176` (`(*glyphPainter).row`); `internal/wire/protocol/name.go:53` (export `IsBidiControl` if needed); Test `attach_render_test.go`.

- [ ] **Step 1:** `TestGlyphPainterRow_DropsC1AndBidi`: cells `'\u009d'`, `'\u009b'`, `'\u202e'` render as spaces; `'é'` as itself. **Step 2:** FAIL. **Step 3:** Replace such runes with `' '` in `row` (also used by `attach_scroll.go:245`). **Step 4:** PASS. **Step 5:** Commit `fix(tui): never print C1 or bidi control runes from the attach screen`.

### Task B4: `rex log` drops controls

**Files:** Modify `internal/surface/cli/inspect/ansi.go:82-88, 142-144`; Test `log_test.go`, `log_fuzz_test.go`.

- [ ] **Step 1:** `TestStripANSI` gains `"a\x07b"`→`"ab"`, `"a\rb\n"`→`"ab\n"`, `"a\u009bb"`→`"ab"`, `"a\xffb"`→`"a\ufffdb"`, `"a\u202eb"`→`"ab"`; `FuzzStripANSI` asserts no byte < 0x20 except `\t`/`\n` and no C1 rune in the output.
- [ ] **Step 2:** FAIL. **Step 3:** Decode UTF-8 in `ground()`, keeping partial runes across writes. **Step 4:** PASS, including `TestStripWriter_everySplitPoint`. **Step 5:** Commit `fix(log): drop control characters and invalid UTF-8 from rex log`; CHANGELOG; `docs/cli.md` (`attach`, `log`); gate; merge.

---

## Part C — M7: complete uninstall (branch `harden/m7-uninstall-all`)

### Task C1: `-all` removes rex data, cache, logs and workspace files

**Files:** Modify `internal/surface/cli/setup/uninstall.go:143-174` (plan), `:302-336` (`execute`); Test `uninstall_test.go` (fixture `newUninstallFixture`, :33).

**Interfaces:** Consumes `paths.DataDir`, `paths.CacheDir`, `paths.WorkspacesFile`, `workspace.OpenRegistry(path).List()`, `workspace.DirName`, `workspace.IndexFileName`, `workspace.AuditFileName`, `workspace.FileName`, `workspace.ConfigFileName`, the rexlog file naming. Produces plan fields `removeDirs []string`, `removeFiles []string`, `keep []string`.

- [ ] **Step 1:** Tests: `TestRunUninstall_AllRemovesDataAndCache` (`~/.local/share/rex/{audit.jsonl,usage.jsonl,snapshots/x,exports/y}`, `~/.cache/rex/z` gone after `-all -yes`); `TestRunUninstall_AllRemovesWorkspaceIndexAndAudit_KeepsConfig` (only `config.lua` and `workspace.json` remain, both listed as kept); `TestRunUninstall_AllSkipsSymlinkedWorkspaceRex` (the target of a symlinked `.rex` keeps its `sessions.jsonl`; output says skipped); `TestRunUninstall_AllListsProfileBackups` (`.zshrc.rex-backup-1` kept and listed); `TestRunUninstall_DryRunListsEverything`.
- [ ] **Step 2:** FAIL. **Step 3:** Read the workspace registry before planning; every removal passes `isUnder` (:203) and `os.Lstat` (skip symlinks). **Step 4:** PASS. **Step 5:** Commit `fix(uninstall): -all removes all rex data, cache and workspace records`.

### Task C2: Hint, help and docs

**Files:** `uninstall.go:143` (`-wipe-state` help), `:268` (closing hint); `docs/cli.md`; `CHANGELOG.md`.

- [ ] **Step 1:** `TestRunUninstall_ClosingHintNamesAllFolders` (names `~/.local/share/rex` and `~/.cache/rex`). **Steps 2–4:** FAIL, update, PASS. **Step 5:** Commit `docs(uninstall): name every folder rex writes`; gate; merge.

---

## Part D — M3: attribution (branch `harden/m3-attribution`)

### Task D1: Peer executable path

**Files:** Create `internal/daemon/server/procexe_darwin.go`, `procexe_linux.go`, `procexe_other.go`; Modify `peerproc.go`; Test `peerproc_test.go`.

**Interfaces:** Produces `func procExe(pid int) (string, error)`; `peerClass.exe string` (empty when unknown; never changes `inSession`).

- [ ] **Step 1:** `TestProcExe_Self`: on Linux equals `filepath.EvalSymlinks(os.Executable())`; on macOS `filepath.Base` equals that of `os.Executable()` (the path is as given to exec). `TestProcExe_GoneProcessFails`.
- [ ] **Step 2:** FAIL. **Step 3:** darwin `unix.SysctlRaw("kern.procargs2", pid)`: skip the 4-byte argc, read to the first NUL; linux `os.Readlink("/proc/<pid>/exe")`. **Step 4:** PASS. **Step 5:** Commit `feat(server): read the peer's executable path`.

### Task D2: Log who did what

**Files:** Modify `internal/daemon/server/client.go` (`client`, `dispatch`, `handleClient`), `handlers.go` (`handleSendInput` :227, `handleReply`); Test `attribution_log_test.go` (new; a capturing `slog` handler installed and restored by the test).

**Interfaces:** Produces `var readOnlyIntents = map[string]bool{Hello, Subscribe, Resize, Boards, UsageQuery, FocusFilter, UsageSample}`; `client.announced bool`; `client.inputBytes map[string]int`.

- [ ] **Step 1:** `TestDispatch_LogsFirstIntentWithPeer` (one Info "server: client" with `pid`, `exe`, `in_session`, `client` after `Hello`; none for a connection that sends nothing); `TestDispatch_LogsEveryNonReadOnlyIntent` (loop over `defaultHandlers()`: each key not in `readOnlyIntents` produces an Info record with its `type`); `TestInput_LogsBytesNeverContent` (two `SendInput` of `"secret-123"`: one "first input" record, one disconnect record with `bytes=20`; no record text contains `secret-123`).
- [ ] **Step 2:** FAIL. **Step 3:** Implement. **Step 4:** PASS. **Step 5:** Commit `feat(server): log which process connected and every intent it sent`.

### Task D3: Requester in session meta and audit

**Files:** Modify `internal/daemon/state` (`Session`, meta.json), `internal/features/audit/audit.go:38` (`Record`) and add `AppendDelete`, `internal/daemon/boot/audit.go:78-173`, `internal/daemon/server/handlers.go` (NewSession, Resume), `retire.go:47-74`, the `rex audit` reader; Tests `boot/audit_test.go`, `server/audit_order_test.go`, `features/audit` tests, the `rex audit` command test.

**Interfaces:** Produces `type Requester struct { Client string \`json:"client,omitempty"\`; PID int \`json:"pid,omitempty"\`; Exe string \`json:"exe,omitempty"\` }` in package `state` (lowest layer; `audit` may import `state`, never the reverse); `Session.RequestedBy *state.Requester \`json:"requested_by,omitempty"\``; `Record.RequestedBy *state.Requester`; `func AppendDelete(path string, sessionID string, by *state.Requester, at time.Time) error` writing `{"event":"delete","session_id":…,"deleted_by":…,"ts":…}`.

- [ ] **Step 1:** `TestSpawn_StoresRequesterInMeta` (also with an empty prompt); `TestAudit_RecordCarriesRequestedBy` (end-of-run record has `requested_by`); `TestDeleteSession_AppendsDeleteLine` (after a finished session is deleted, `audit.jsonl` has a second line with `"event":"delete"` and `deleted_by`); `TestAuditReader_ShowsDeleteLines` (`rex audit` lists it, and an older reader's fields still parse).
- [ ] **Step 2:** FAIL. **Step 3:** Thread the requester from `client.peer` and the Hello client name. **Step 4:** PASS; document both in the audit docs. **Step 5:** Commit `feat(audit): record who started and who deleted a session`; CHANGELOG; gate; merge.

---

## Part E — M1: allowlisted environment (branch `harden/m1-child-env`)

### Task E1: `childenv` package

**Files:** Create `internal/daemon/childenv/childenv.go`, `childenv_test.go`.

**Interfaces:** Produces `type Mode string` (`ModeAllowlist = "allowlist"`, `ModeInherit = "inherit"`); `type Options struct { Mode Mode; Pass []string; SSHAgent bool; GOOS string }`; `func Build(parent []string, o Options) []string` (filtered `KEY=VALUE`, order kept); `func BaseNames(goos string, sshAgent bool) []string`; `var sessionMarkers = []string{"CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_SSE_PORT"}`; `const filteredMarker = "REX_DAEMON_ENV_FILTERED"` (always removed from children). Patterns ending in `*` match a prefix; on `windows` names match without case.

- [ ] **Step 1:** Tests: `TestBuild_AllowlistKeepsBaseNames` (`PATH`, `HOME`, `LC_ALL`, `XDG_CONFIG_HOME`, `https_proxy`); `TestBuild_AllowlistDropsOthers` (`ANTHROPIC_API_KEY`, `GH_TOKEN`, `AWS_SECRET_ACCESS_KEY`); `TestBuild_PassthroughAddsNames` (`Pass: ["GH_TOKEN","NVM_*"]` keeps `GH_TOKEN`, `NVM_DIR`); `TestBuild_RemovesSessionMarkersInBothModes`; `TestBuild_KeepsClaudeProviderVars` (`CLAUDE_CODE_USE_BEDROCK`, `CLAUDE_CODE_GIT_BASH_PATH` kept in inherit mode, and in allowlist mode when in `Pass`); `TestBuild_SSHAgentToggle`; `TestBuild_InheritKeepsEverythingElse`; `TestBuild_WindowsBaseKeepsSystemNames` (`systemroot=C:\Windows`, `Path=…`, `ComSpec=…` with `GOOS:"windows"`).
- [ ] **Step 2:** FAIL. **Step 3:** Implement; base lists exactly as in the spec. **Step 4:** PASS. **Step 5:** Commit `feat(childenv): build an allowlisted environment for child processes`.

### Task E2: Registry `env_pass`

**Files:** Modify `internal/catalog/registry/types.go:50`, `loader.go:125-191` (`mergeOne`), `loader.go:229` (`validateTool`), `builtin.yaml`; Test `env_pass_test.go` (new), `fields_test.go`.

**Interfaces:** Produces `Tool.EnvPass []string \`yaml:"env_pass"\`` (name regex of `validateStatusEnv` plus an optional trailing `*`); `func AllEnvPass(r *Registry) []string` (sorted union, for Task E6).

- [ ] **Step 1:** `TestEnvPassValidation` (`"BAD NAME"` refused; `"OK_1"`, `"AWS_*"` accepted); `TestBuiltinEnvPass` (claude has `ANTHROPIC_API_KEY`, `CLAUDE_CODE_USE_BEDROCK`, `CLAUDE_CODE_USE_VERTEX`, `CLAUDE_CODE_GIT_BASH_PATH`; codex has `OPENAI_API_KEY`; no tool has `GH_TOKEN`); `TestMergeNewFields` gains `env_pass`; `TestAllEnvPass_Union`.
- [ ] **Step 2:** FAIL. **Step 3:** Declare each built-in tool's variables, **checking every name against the tool's current documentation** and citing the URL in a YAML comment. **Step 4:** PASS. **Step 5:** Commit `feat(registry): tools declare the environment variables they need`.

### Task E3: Settings

**Files:** Modify `internal/catalog/settings/registry.go` (pattern: `auto_trust_dirs` :386, `board_hidden_sections` :216), `trust_ids.go:14`, `internal/daemon/server/server.go:86`, `internal/daemon/boot/settings_hub.go:297`; Tests: settings tests, `TestTrustSettingIDs`.

**Interfaces:** Produces `child_env` (TypeString `allowlist`/`inherit`, default `allowlist`, `ApplyNextSpawn`), `env_passthrough` (TypeString, `ListSep: ","`, default `""`, `ApplyDaemonRestart`: a name added later is no longer in the filtered daemon), `env_ssh_agent` (TypeBool, default true, `ApplyNextSpawn`), all three in `TrustSettingIDs`; `server.Config.ChildEnv func() childenv.Options` (Pass empty; the spawn adds the tool's).

- [ ] **Step 1:** Defaults; `child_env: nope` refused; all three are trust settings. **Steps 2–4:** FAIL, implement and wire, PASS. **Step 5:** Commit `feat(settings): child_env, env_passthrough and env_ssh_agent`.

### Task E4: Agents get the built environment

**Files:** Modify `internal/daemon/pty/supervisor.go:44-45, 175-177` (add `BaseEnv`), `internal/daemon/server/spawn.go:131, 284-310, 363`, `resume.go:176`; Test `pty/supervisor_test.go`, `e2e/testdata/script/child_env.txtar` (new).

**Interfaces:** Consumes `childenv.Build`, `Config.ChildEnv`, `Tool.EnvPass`. Produces `SupervisorConfig.BaseEnv []string` (nil keeps `os.Environ()`; otherwise `cmd.Env` is always set from it).

- [ ] **Step 1:** `TestSupervisor_BaseEnvReplacesInherited`; e2e `child_env.txtar`: `env SECRET_X=s1`, `daemon-start`, a probe tool runs `env > $WORK/env.out`, `! grep SECRET_X env.out`; then a second daemon with `env_passthrough: SECRET_X` in the script's `config.yaml`, `grep SECRET_X=s1`.
- [ ] **Step 2:** FAIL. **Step 3:** In `startSupervisor`: `o := cfg.ChildEnv(); o.Pass = slices.Concat(tool.EnvPass, o.Pass); BaseEnv = childenv.Build(os.Environ(), o)`; Debug-log the dropped count. **Step 4:** PASS, with `TestSupervisor_PassesEnvToChild` and `TestSupervisor_DropEnv`. **Step 5:** Commit `fix(spawn): agents get an allowlisted environment (child_env)`.

### Task E5: Model probes and loop checks

**Files:** Modify `internal/catalog/registry/models.go:155`, `internal/daemon/boot/catalog.go:121`, `internal/features/loop/check.go:77`, `loop/actor.go:535`; Tests `models_test.go`, `loop/check_test.go`.

**Interfaces:** `runModelsCmd(ctx context.Context, argv []string, env []string)`; `RefreshModels` takes `envFor func(registry.Tool) []string`; `RunCheck(ctx context.Context, argv []string, dir string, timeout time.Duration, env []string)`.

- [ ] **Step 1:** `TestRunModelsCmd_UsesGivenEnv`, `TestRunCheck_UsesGivenEnv`. **Steps 2–4:** FAIL, wire from boot (loop checks: no tool `Pass`), PASS. **Step 5:** Commit `fix(daemon): model probes and loop checks get the allowlisted environment`.

### Task E6: The daemon filters its own environment

**Files:** Create `internal/daemon/boot/ownenv.go` (unix) and `ownenv_windows.go` (no-op, documented); Modify `cmd/rex-daemon/main.go` (call first in `run`, before any file or socket is opened); Test `boot/ownenv_test.go`, `e2e/testdata/script/child_env.txtar` (a `[linux]` block).

**Interfaces:** Produces `func OwnEnv(parent []string, opts childenv.Options, toolPass []string) (env []string, needExec bool)` (pure: `needExec` is false when `REX_DAEMON_ENV_FILTERED=1` is set or `Mode` is inherit; `env` = `Build(parent, opts with Pass = slices.Concat(toolPass, opts.Pass))` plus the marker); `func ReexecFiltered(exec func(string, []string, []string) error) error` (loads settings and `registry.AllEnvPass`, calls `OwnEnv`, then `exec(os.Executable(), os.Args, env)` once).

- [ ] **Step 1:** `TestOwnEnv_FiltersAndMarks`, `TestOwnEnv_NoExecWhenMarked`, `TestOwnEnv_NoExecWhenInherit`; e2e `[linux]`: after `daemon-start` with `SECRET_X=s1`, `! exec grep -a SECRET_X /proc/<daemon pid>/environ` (pid from `rex daemon status -o json`).
- [ ] **Step 2:** FAIL. **Step 3:** Implement; hot restart (`server/hot.go:363`) needs no change, since `os.Environ()` is already filtered. **Step 4:** PASS. **Step 5:** Commit `fix(daemon): filter the daemon's own environment at startup`; CHANGELOG (**Changed**, breaking, migration note: set `child_env: inherit` or list names in `env_passthrough`, then restart the daemon); `docs/settings.md`; `SECURITY.md` (closes S-06; states the remaining Linux exposure and what `SSH_AUTH_SOCK` gives away); gate; merge.

---

## Part F — M2: first prompt by paste (branch `harden/m2-prompt-paste`)

### Task F1: `prompt_paste.ready_regex`

**Files:** Modify `internal/catalog/registry/fields.go:49-54` (`PromptPaste`), `loader.go` (validate), `builtin.yaml` (claude); Create `internal/catalog/registry/testdata/claude-ready.txt`, `claude-trust.txt`; Test `fields_test.go`.

**Interfaces:** Produces `PromptPaste.ReadyRegex string \`yaml:"ready_regex"\`` (must compile). Because it lives inside `prompt_paste`, `mergeOne`'s existing swap of `prompt_args`/`prompt_paste` (`loader.go:176-183`) replaces it too.

- [ ] **Step 1:** `TestReadyRegexValidation` (a bad regex is refused); `TestBuiltinPromptDelivery` updated (claude uses `prompt_paste` with `ready_regex`, no `prompt_args`); `TestClaudeReadyRegex_Samples` (matches `claude-ready.txt`, not `claude-trust.txt`); `TestMerge_UserPromptArgsReplacesPaste` (a user `prompt_args` for claude loads cleanly).
- [ ] **Step 2:** FAIL. **Step 3:** The two sample screens come from a real Claude Code session: **ask the maintainer for them** (a fresh session waiting for input, and one showing the trust dialog) instead of guessing. **Step 4:** PASS. **Step 5:** Commit `feat(registry): prompt_paste.ready_regex`.

### Task F2: Paste only when ready, never into a dialog, and wait out a slow answer

**Files:** Modify `internal/daemon/pty/supervisor.go:354-414` (paste goroutine; reads the rendered screen the way the trust answerer does at :789), `SupervisorConfig`; `internal/daemon/server/spawn.go`, `spawn_prompt.go:31`; Test `pty/supervisor_test.go`.

**Interfaces:** Produces `SupervisorConfig.ReadyRe *regexp.Regexp`, `TrustShownRe *regexp.Regexp`, `OnPromptUndelivered func()`; `const promptReadyDeadline = 30 * time.Second` (counts only time with no dialog shown) and `const promptHardCap = 10 * time.Minute`.

- [ ] **Step 1:** Tests (fake child printing scripted screens; deadlines overridable in tests):
  - `TestSupervisor_PromptWaitsForReadyRegex`.
  - `TestSupervisor_PromptNeverTypedIntoTrustDialog`: ready pattern and trust prompt both on screen → nothing written; dialog gone → one paste.
  - `TestSupervisor_PromptDeadlinePausedWhileDialogShown`: deadline 1 s; the dialog stays 3 s, then closes → the prompt is still pasted.
  - `TestSupervisor_ClosedDialogNoLongerBlocks`: the dialog text scrolls out of the raw output window's history but the redraw removed it from the screen → paste goes out (proves the rendered screen is used).
  - `TestSupervisor_PromptUndeliveredCallsBack`: never ready → `OnPromptUndelivered` once, nothing written.
- [ ] **Step 2:** FAIL. **Step 3:** Ready = (adapter ready **or** `ReadyRe` matches the rendered screen) **and not** `TrustShownRe` matches it **and** idle ≥ settle; the deadline clock stops while `TrustShownRe` matches. Keep `promptPending` / `errStarting`. **Step 4:** PASS, with the existing `TestSupervisor_InitialPrompt*` and `TestFreezeRefusedWhileStarting`. **Step 5:** Commit `fix(pty): paste the first prompt only when the agent is ready, never into a dialog`.

### Task F3: Undelivered notice; Windows launch path

**Files:** Modify `internal/daemon/state` (session flag), `internal/wire/protocol/events.go` (`SessionSummary.PromptUndelivered bool \`json:"prompt_undelivered,omitempty"\``), the TUI card, `internal/daemon/server/launch_plan.go:25`; Tests `server/launch_plan_test.go`, `winlaunch_test.go`, a TUI snapshot test.

- [ ] **Step 1:** `TestSpawn_PromptUndeliveredSetsFlag`; the TUI card shows `first prompt not delivered`; `planLaunch` still works for claude on Windows (existing tests updated, not deleted). **Steps 2–4:** FAIL, implement, PASS. **Step 5:** Commit `feat(tui): say when the first prompt could not be delivered`; CHANGELOG; `docs/registry.md` (`prompt_paste.ready_regex`); `TODO.md` (one line per remaining argv tool); gate; merge.

---

## Part G — M6: tags only from master (branch `harden/m6-release-tags`)

### Task G1: Refuse tags whose commit is not on master

**Files:** Create `tools/release/tag_on_master.sh`, `tools/release/tag_on_master_test.go` (or `_test.sh`); Modify `.github/workflows/release.yml` (checkout with `fetch-depth: 0`, call the script before building).

- [ ] **Step 1:** Test builds a temp git repo with a commit on `master` and one off it; the script exits 0 and 1 respectively, with a message naming the tag. **Step 2:** FAIL. **Step 3:** `git merge-base --is-ancestor "$1" origin/master`. **Step 4:** PASS; `actionlint` if installed. **Step 5:** Commit `ci(release): refuse tags whose commit is not on master`.

### Task G2: Say what a release check proves

**Files:** Modify `SECURITY.md:192`, `README.md:210`, `CHANGELOG.md`; Create `docs/plans/release-signing.md` (a stub spec holding the deferred items from M6, so they are not lost).

- [ ] **Step 1:** No code; review the wording against the spec's M6 section. **Step 2:** Commit `docs(security): what a release checksum proves, and the signing plan`; gate; merge.
