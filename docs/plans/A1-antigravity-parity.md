# A1 Antigravity parity

Status: planned (PL7). Hooks decision pending (user chose: options first, decide later).

## Summary
PR #15 already covers spawn, auto-trust and footer state for `agy`. The real gaps are: questions are detected late, there is no resume id, there is no subagent count, and the summary text still contains agy's screen chrome. Hook-based state is one way to close them, but it needs a global config write.
Running `strings` on agy 1.5.5 turned up two per-session channels that need no global writes: **OSC 7501 terminal status** (turned on by setting `AGY_CLI_OSC7501=on` in the session's environment) and an **exit line** that prints `agy --conversation=<id>`. Recommended: build on these and hold off on hooks. Whether to use hooks is still the user's decision.
Work before the hooks decision: about M (4 to 6 commits). Work after, only if hooks are chosen: about M.

## How the facts were checked
- **Verified from `agy --help` (1.5.5):** `--settings` ("session only, never saved: a JSON object of settings.json keys"), `--prompt-interactive`/`-i`, `--conversation` ("Resume a previous conversation by ID"), `-c/--continue`, `--effort low|medium|high|xhigh|max`, `--mode accept-edits|auto|plan`, `--log-file`, `--add-dir`, `agy plugin {install,uninstall,enable,disable,validate,list}`.
- **Verified from the hooks guide and changelog embedded in the agy binary** (`strings ~/.local/bin/agy`). Nothing under `~/.gemini` was read beyond listing `config/` and the key *names* in `config/config.json`:
  - **Hook files:** hooks live only in `hooks.json` files: global `~/.gemini/config/hooks.json`, per-workspace `<ws>/.agents/hooks.json` (loaded once the folder is trusted), and per-plugin `~/.gemini/config/plugins/<name>/hooks.json`. The top level is `{ "<hook-name>": { "enabled"?, "<Event>": [...] } }`, and named hooks from different files merge.
  - **Events:** `SessionStart`, `PreToolUse`, `PostToolUse` (grouped with `matcher`), `PreInvocation`, `PostInvocation`, `Stop` (flat). There is **no `Notification`** (permission) event and **no subagent** events.
  - **Commands:** run with `sh -c` (Unix) or **`cmd /c` (Windows)**. Working directory is the folder holding hooks.json. Timeout defaults to 30 s. They run **synchronously and block the agent loop**.
  - **Input and output:** input is JSON on stdin with `conversationId`, `cwd`, `hookEventName`, `transcriptPath`, …. Output must be JSON on stdout. `PreToolUse` **requires** a `decision`, so rex must not register it. `Stop` passes `terminationReason` (includes `USER_CANCELED`, which covers Esc).
  - **OSC 7501:** changelog: *"terminal status reporting over OSC 7501 … working, waiting for your input, done, or failed … `AGY_CLI_OSC7501=on` for a terminal that supports it but doesn't answer the check, `AGY_CLI_OSC7501_MESSAGES=brief` …"*. The query string `ESC]7501;?` is present in the binary.
  - **Exit text:** `"Resume with -c (or command below):"` followed by `"agy --conversation=%s"`.
- **Unverified (needs one live capture, see open question 2):**
  - the OSC 7501 payload (word order, status words, BEL or ST terminator);
  - how the exit line is actually rendered;
  - the text of the permission/question dialog;
  - which glyph marks the selected option in the trust dialog;
  - whether hook commands inherit the agy process environment (`REX_HOOK_FILE`). "Plugin hooks *also* get PLUGIN_ROOT" suggests they do.
  - No stored agy transcripts were found under `~/.local/share/rex/sessions` to check against.
- Baseline is green: `go test` passes for adapter, registry and the server trust/prompt tests.

## Parity matrix

| # | Integration | Claude Code today | agy today | Gap |
|---|---|---|---|---|
| 1 | Auto-trust | `trust_prompt` answerer: `builtin.yaml:44-48`, `server/trust.go:17-38`, `pty/trust.go` (60 s window) | `trust_args` session-only `--settings {"trustedWorkspaces":…}`: `builtin.yaml:144`, `server/trust.go:44-66` | **None in practice.** Optional fallback `trust_prompt` in case a future agy ignores `--settings`; needs the selection glyph from a capture. |
| 2 | State detection | Exact hooks: `adapter/claude_hooks.go:30-41,112-133`; spawn injects them at `server/spawn.go:72-85` | Footer heuristic `? for shortcuts` / `esc to cancel`: `builtin.yaml:131-141`, `adapter/heuristic.go:101-120` | **Permission/question prompts read "working" for up to 15 s** (`staleWorking`, `heuristic.go:89`). Fragile to footer wording changes. The trust dialog outside auto-trust dirs also takes 15 s to show as waiting. |
| 3 | Esc-interrupt | 15 s silence guard: `claude_hooks.go:25,167` | Footer swaps back to idle | OK |
| 4 | Subagent count (PR #12) | `SubagentStart/Stop`, `claude_hooks.go:129-130,176-187`; supervisor reads it at `pty/supervisor.go:389,464` | none | **No source.** agy hooks have no subagent events. Swarm workers can only be counted by scraping the screen (format unverified). Defer. |
| 5 | Summarizer/last_line cleaning | Claude chrome in `termtext.go:110-135`; `❯` input row cut in `ScreenLines`, `termtext.go:79-97` | `? for shortcuts` and `esc to cancel` are filtered | **"Generating..." spinner line and agy's `>` input row (typed text) leak** into last_line and the summary. Owned by S1–S3; A1 supplies fixtures. |
| 6 | Initial prompt | `-- <prompt>`: `spawn_prompt.go:30-32` | `--prompt-interactive=<prompt>`: `spawn_prompt.go:33-36` (verified in help) | OK |
| 7 | Model / effort args | `--model alias` + `--effort=`: `builtin.yaml:51-71` | slug, plus `--effort` or an effort-picked slug: `builtin.yaml:148-170` (repeated `--model`: last one wins, verified in PR #15) | Model list is static, but agy slugs depend on the account (`agy models`). P1 may want "refresh from `agy models`". No code gap. |
| 8 | Resume id | none for any tool (no field in `state/session.go:12-60`) | none | **agy prints `agy --conversation=<uuid>` on exit**, and `--conversation` resumes. Can be captured from the exit output. T3 owns the field and column. |
| 9 | Attach/render | `altScreenTools` `tui/attach.go:84-92`, `redrawKickTools` `cli/session/attach.go:43-51` | Same maps include `antigravity` | OK. The maps are hardcoded by tool id; U3 edits them. The #16 follow-ups (`?2004` not tracked) apply to both tools equally. |
| 10 | Argv log redaction | `pty/argv.go:36-65` | Same; the attached `--prompt-interactive=` value and the `--settings` value are redacted | OK |
| 11 | Doctor | PATH lookup only: `setup/doctor.go:186-199` | `agy` PATH lookup only | **No version shown** (verified with 1.5.5). No status-channel or hooks-install row. |
| 12 | Setup wizard trust text | `tui/setup_wizard.go:1217` says "Claude Code asks…" | not mentioned | Copy should name both. Owned by P2/P1. |
| 13 | Permission mode | not exposed | `--mode` exists | Out of scope (no parity gap). |

## Hook-based state: options (USER DECISION)

### (a) Write the global `~/.gemini/config/hooks.json` once, with user approval
**Content.** Rex adds one key; every other key stays as it is:
```json
{
  "rex-state": {
    "SessionStart":   [{"type":"command","timeout":5,"command":"<SS>"}],
    "PreInvocation":  [{"type":"command","timeout":5,"command":"<W>"}],
    "PostToolUse":    [{"matcher":"*","hooks":[{"type":"command","timeout":5,"command":"<W>"}]}],
    "Stop":           [{"type":"command","timeout":5,"command":"<NI>"}]
  }
}
```
`<W>` and `<NI>` are `[ -n "$REX_HOOK_FILE" ] && { printf 'working\n' >>"$REX_HOOK_FILE"; } 2>/dev/null; printf '{}'; exit 0` (with `needs_input` in place of `working` for `<NI>`). `<SS>` also extracts `conversationId` with the existing `hookJSONField` and `hookSafeID` (`claude_hooks.go:67-72`) and writes `conv <id>`. No `PreToolUse`, because its output requires a `decision`.

**Install.** Parse the file as `map[string]json.RawMessage`. Refuse if it does not parse. Back it up once to `hooks.json.rex-backup-<ts>`. Set only `rex-state`. Write atomically (temp file plus rename) and keep the file mode.

**Uninstall.** `rex uninstall` (and `rex setup -no-agy-hooks`) deletes only `rex-state`. It removes the file only if rex created it and it is now `{}`.

- **Pros:** exact working and turn-end events; `conversationId` at session start, so resume works mid-run; Esc is covered by `Stop`.
- **Cons:**
  - It is a global file that agy treats as a guardrail file.
  - It runs on **every** agy session, including ones outside rex. The guard makes them no-ops, but each one still costs one `sh` per model call and per tool, synchronously.
  - It still has no permission event, so it does not fix row 2's main gap.
  - Windows runs `cmd /c`, so the `sh` one-liners fail there and Windows must fall back.
  - Merge conflicts with the user's own hooks are possible.
- **Security:** rex starts editing another tool's security config. Code runs in all agy sessions. The payload is never put into the shell (fields are cut with sed and ids sanitized with `tr -cd`). The agent's child commands inherit `REX_HOOK_FILE` and could fake state, the same as Claude today. SECURITY.md must add "rex changing hooks.json beyond its key" to the in-scope list.

### (b) Per-session hooks through a flag, env var or config-dir override
- **`--settings`:** takes *settings.json* keys. Hooks are documented only in `hooks.json` (embedded guide); the PR #15 author found the same. Not available.
- **`ANTIGRAVITY_CONFIG_REL_DIR`:** an env var in the binary that looks like a config-dir relocation. **Unverified and undocumented.** Would likely move MCP, plugins and maybe auth too. Not recommended.
- **Workspace `<cwd>/.agents/hooks.json`:** per folder, not per session. Writes into the user's repos (git noise) and affects sessions outside rex. Rejected.

**Verdict:** no supported per-session hook path in 1.5.5.

### (c) Stay footer-based and make it more robust
Add an `ask_regex` heuristic field for agy's question/permission dialog: if it appears after the last busy match, the state is `needs_input` immediately. Optionally add the agy `trust_prompt` fallback. Footer wording stays the main risk.
- **Pros:** no writes, small change.
- **Cons:** the dialog text is unverified (needs a capture), and screen scraping stays fragile.
- **Security:** nothing new.

### (d1) OSC 7501 status channel, per session, env only (recommended primary)
At spawn, rex sets `AGY_CLI_OSC7501=on` in the child env (`spawn.go:64-86`). agy then emits explicit **working / waiting for input / done / failed** sequences, including when it waits on a pending command or question, into the PTY rex already reads.
- **Pros:**
  - Per session; nothing is written outside rex.
  - Fixes row 2's 15 s question lag.
  - Works the same on Windows.
  - Its message detail (the pending question) could later feed the board's "needs you" preview.
- **Cons:** the payload format is unverified until one capture is made, and the feature may change across agy versions. The footer heuristic stays as the fallback.
- **Security:** env only. Rex already passes these bytes through on attach; vt10x ignores unknown OSC (`third_party/vt10x/str.go:137`). The parser must bound its memory, and any message text shown must go through `sanitizeForDisplay`.

### (d2) A rex-owned agy plugin, `~/.gemini/config/plugins/rex/{plugin.json,hooks.json}`
Same hooks as (a), but in rex's own folder.
- **Pros over (a):** no merge with the user's hooks.json and no backup needed. `agy plugin disable rex` is a built-in off switch, uninstall is "delete the folder" (or `agy plugin uninstall rex`), and `agy plugin validate` gives doctor a check.
- **Cons:** still global and on every session. Whether a folder can be dropped in or must go through `agy plugin install <local path>` is unverified. Same Windows issue as (a).
- **Security:** as (a), but it never rewrites a file the user owns.

### (d3) Exit-line resume capture (not state)
Scrape `agy --conversation=<uuid>` from the exit output (row 8). No hooks needed.

### Rejected
`agy acp` (ACP stdio) and `--output-format stream-json` (print mode only) both replace agy's TUI, which breaks attach.

### Recommendation (USER DECISION, approval for (a) is not assumed)
Do **(d1) + (c) fallback + (d3)** now. Do not touch global agy config. Revisit hooks only if the OSC capture fails, or if the resume id is wanted *while* a session runs. If hooks are wanted, prefer **(d2) plugin over (a)**.

## Design for gaps the hooks decision does not block

1. **OSC status adapter (row 2):**
   - **Registry:** new optional `Detect.Status string` (`yaml:"status"`). The only accepted value is `"osc7501"`. Validate it in `registry/loader.go:155-185`. The builtin antigravity entry gets `status: osc7501`.
   - **Spawn:** when `tool.Detect.Status == "osc7501"`, append `AGY_CLI_OSC7501=on` to `childEnv`. Duplicate keys: the last one wins, so it overrides a user `off`; document this. To make it testable, pull the env block into a pure `childEnvFor(tool, hookFile, id) []string`.
   - **Adapter (new file `adapter/osc_status.go`):**
     - `type OutputObserver interface{ Observe(chunk []byte) }`, called from the PTY reader goroutine.
     - `oscStatus` wraps `*HeuristicCLI`: a mutex, a bounded carry buffer (≤512 B) for sequences split across reads, a `last` state, and a `seen` flag.
     - Status mapping: working → `working`; waiting, done or failed → `needs_input`. `done` is the turn ending; process exit still decides `done`/`failed`.
     - Before any status is seen, delegate to the heuristic.
     - Keep the 15 s no-output guard on `working`, consistent with `claude_hooks.go:167`.
     - `IsReadyForInput` delegates to the heuristic.
   - **Factory:** `adapter.For` (`adapter.go:25-46`) wraps the heuristic when Status is set.
   - **Supervisor:** one type assertion next to `SubagentCounter` (`supervisor.go:389`), and `obs.Observe(chunk)` after `screen.Write` (`supervisor.go:345`), outside `outMu`.
2. **Resume id for agy (row 8):** registry `resume: {capture_regex, args}`. agy values: `capture_regex: "agy --conversation=([0-9a-fA-F-]{36})"`, `args: ["--conversation={id}"]`. Capture at child exit (`supervisor.go:425-428`) from `termtext.Normalize(window)`, because the line prints after the alt screen closes. **Interface agreement with T3:** T3 owns `Session.ResumeID`, the wire field, the column, the resume intent and the generic capture. A1 supplies agy's values and fixture. If T3 prefers no regex mechanism, A1 ships the capture in the same shape. Validate the id against `^[0-9a-f-]{36}$` and pass it as one argv element.
3. **Doctor (row 11):** new `checkAgyVersion()` appended in `runAllChecks` (`doctor.go:92-104`). It runs `agy --version` with a 2 s timeout through an injectable `var agyVersion func(context.Context) (string, error)`. Info row `agy 1.5.5 (status: osc7501)`; warn below 1.5.5 ("untested"). The `checkToolBinaries` list (U3's) is left alone.
4. **After the capture:** an agy `trust_prompt` fallback (row 1), and `ask_regex` only if the OSC capture fails (option c).
5. **Summarizer (row 5):** hand S1 the synthetic agy screen fixtures (`Generating...` row, `>` input row with typed text, footer). A1 does not edit `termtext.go` unless S1 asks.

## Files to change
**Before the decision:**
- `internal/catalog/registry/{types.go,loader.go,builtin.yaml (antigravity block only, lines 123-170),trust_test.go/loader_test.go}`
- `internal/daemon/adapter/{adapter.go (For), osc_status.go (new), osc_status_test.go (new), factory_test.go}`
- `internal/daemon/pty/{supervisor.go (reader at ~345 and the assertion at ~389), supervisor_test.go}`
- `internal/daemon/server/{spawn.go (env block 64-86 → childEnvFor), spawn_env_test.go (new)}`
- `internal/surface/cli/setup/{doctor.go, doctor_test.go}`
- Docs.

**After the decision ((a) or (d2)):**
- `adapter/claude_hooks.go`: pull out a shared hook-file reader into `hookfile.go`.
- `adapter/agy_hooks.go` (new; decorator with OSC/footer fallback).
- `server/spawn.go`: create the hook file and set `REX_HOOK_FILE` for `detect.hooks: antigravity`.
- `setup/agyhooks.go` (new; install, uninstall, merge).
- `setup/uninstall.go`, `setup/setup.go` (flag).
- `doctor.go` (installed / stale / disabled row).
- `SECURITY.md`.

## Test plan (test-first, no live agy in tests, synthetic bytes only)
- **`osc_status_test.go`:**
  - table-driven parse cases: BEL and ST terminators; split at every byte boundary; garbage; oversized payload (carry is capped); unknown status ignored;
  - precedence: no OSC → heuristic result; OSC working beats an idle footer; OSC waiting beats the busy footer immediately (the 15 s regression); stale guard;
  - Observe and Detect running concurrently, under `-race`.
  - Must keep the adapter floor at 98 %.
- **`factory_test.go`:** builtin antigravity builds the OSC adapter. `loader_test`: an unknown `status` value is rejected; merge behaviour (the Detect block is replaced wholesale, as today).
- **`supervisor_test.go`:** a fake adapter implementing `OutputObserver` receives every chunk in order (existing fake-adapter pattern).
- **`spawn_env_test.go`:** `childEnvFor` includes `AGY_CLI_OSC7501=on` only for `status: osc7501`, and `REX_HOOK_FILE` only for hooks tools. Pure function, so no Windows process issues.
- **Resume:** normalized synthetic exit block (`Resume with -c…\r\nagy --conversation=<fake uuid>` with SGR codes) gives the id; ids that are not UUIDs are rejected.
- **Doctor:** an injected `agyVersion` returning ok, old, error and timeout.
- **After the decision:**
  - merge tests under `t.Setenv("HOME")` and `USERPROFILE`: file absent; other keys kept; rex key replaced; malformed file refused; backup made once; uninstall removes only the rex key; mode kept;
  - hook commands run through `sh -c` with a fixture payload on stdin (skip on Windows): they write lines and print `{}`, and are no-ops without `REX_HOOK_FILE`.
- **Gate:** `make check` per commit. Raise coverage floors if they go up.

## Docs and CHANGELOG
- `docs/registry.md`: `detect.status`, `resume`, the antigravity paragraph.
- `docs/agents/modules/adapter.md` and `pty.md`: `OutputObserver`.
- `docs/cli.md`: doctor row.
- `docs/agents/HANDOFF.md` §5 item 2: point it at this decision.
- `TODO.md`: A1 progress.
- `CHANGELOG.md` `[Unreleased]`: "Antigravity sessions show waiting at once on a question (OSC 7501)", "rex doctor shows the agy version".
- After the decision: SECURITY.md (the in-scope list, hooks/plugin file), and docs/settings.md or setup docs for the install and uninstall flag.

## Conflict map vs parallel tasks
- **U3 (builtin.yaml):** A1 touches only the `antigravity` block. Flag to U3: removing the hidden `gemini` entry brings back the PR #15 overlay/default breakage (commit ab92413). The hardcoded id maps (`tui/attach.go:84`, `cli/session/attach.go:43`, `doctor.go:187`, `keymap.go:214`) belong to U3; A1 does not edit them.
- **T3 (resume):** T3 owns the field, wire, column, intent and generic capture. A1 owns agy's `resume` values and fixture. Both edit `spawn.go` (`cmdArgs` 52-62 for T3, env 64-86 for A1) and the `supervisor.go` exit path (425-440). A1's `childEnvFor` extraction should land first or rebase. If T3 adds Claude `session_id` capture in `claude_hooks.go`, the post-decision hook-reader refactor rebases on it.
- **S1–S3 (termtext/summarizer):** they own `termtext.go`; A1 gives them fixtures only.
- **P1/P2 (setup wizard, trust):** `setup_wizard.go:1217` copy should mention Antigravity. Any hooks install step after the decision would go into P1's wizard; agree on ordering.
- **U1/U2 (UI):** no overlap.
- **`daemon/adapter`:** A1 adds new files and edits `For()` only. **`spawn_prompt.go`:** not touched.

## Security notes
- Before the decision: no writes outside rex dirs. A new env var in the child. The OSC parser has bounded memory and ignores bad input. Any OSC message text shown in the UI must go through `sanitizeForDisplay` (SECURITY.md "untrusted bytes"). Resume ids are validated as UUIDs and passed as single argv elements.
- Never commit live captures (HANDOFF §3); fixtures are synthetic.
- After the decision: see the (a)/(d2) security bullets. The guard makes hooks no-ops outside rex. No payload goes into the shell. Writes are atomic with a backup. SECURITY.md is updated.

## Open questions (each with a recommended default)
1. **Hooks:** (a) global hooks.json, (d2) rex plugin, or none? **Default: none for now.** If later, (d2).
2. **One live agy capture** (tiny prompt, scratch temp folder, raw bytes kept local and never committed) to confirm the OSC 7501 payload, the exit line, the question dialog and the trust glyph? **Default: yes.** Steps 4-6 are blocked without it.
3. Force `AGY_CLI_OSC7501=on` even if the user's shell sets `off`? **Default: yes, documented.**
4. OSC messages full or `brief`? **Default: full** (enables a question preview later).
5. Add the agy `trust_prompt` fallback? **Default: yes, after the capture.**
6. Windows hooks (if 1 = yes): **Default: Unix only; Windows uses OSC/footer.**
7. Resume capture owner: **Default: T3 generic, A1 supplies agy values.**
8. Doctor warning below agy 1.5.5? **Default: yes (info plus warn "untested").**

## Ordered steps (one commit each; each builds and passes `make check`)
**Before the hooks decision:**
1. `refactor(server): build child env in childEnvFor`: pure extraction plus test.
2. `feat(doctor): show the agy version`: `checkAgyVersion` plus tests and docs/cli.md.
3. `test(termtext): agy screen fixtures`: only if S1 wants them in-tree. Otherwise hand them over.
4. *(capture with consent, Q2, no commit)* → `feat(adapter): agy state from OSC 7501 status`: registry field and validation, `childEnvFor` env, `oscStatus` and `OutputObserver`, supervisor hook, builtin `status: osc7501`, tests, registry/adapter/pty docs, CHANGELOG.
5. `feat(registry): capture agy's resume id on exit`: after T3's field lands, or per the agreement.
6. `feat(registry): agy trust_prompt fallback` (Q5). If step 4's capture showed OSC is unusable, it becomes `feat(adapter): ask_regex for agy questions` (option c).
7. `docs(handoff): A1 status and hooks decision pending`.

**After the decision (only if (d2) or (a) is approved):**
8. `refactor(adapter): share the hook-file reader`.
9. `feat(setup): install/uninstall rex agy hooks (plugin|merge)` with backup, atomic write, `rex uninstall` integration, and a doctor row.
10. `feat(adapter): antigravity hook state with OSC/footer fallback`: spawn sets `REX_HOOK_FILE`, `SessionStart` passes `conv <id>` to T3's ResumeID, SECURITY.md, CHANGELOG.

## Size estimate
- Before the decision: **M** (steps 1-7; step 4 is the bulk).
- After the decision: **M**.
- Overall **M**, or **L** if both are done.

## Critical files
- internal/daemon/adapter/adapter.go (plus new osc_status.go)
- internal/daemon/pty/supervisor.go
- internal/daemon/server/spawn.go
- internal/catalog/registry/builtin.yaml (antigravity block) and types.go/loader.go
- internal/surface/cli/setup/doctor.go

## Live capture findings (2026-11-11, D2; agy 1.5.6, TERM=tmux-256color, AGY_CLI_OSC7501=on)

Raw bytes stayed in the session scratchpad and were not committed. Facts only:

- **OSC 7501 format:** `ESC ] 7501 ; <fields> BEL` (BEL-terminated in every sample). Fields are colon-separated `key=value`: always `state=<word>` and `app=antigravity`; optional `kind=<word>` and `msg=<base64>` (standard base64 of a human message — untrusted text: decode, then `SanitizeDisplayText` before any display; never log it).
- **States observed:** `working`, `idle`, `blocked` (seen with `kind=auth`), `error` (seen with the quota message). Map: working → working; idle → needs_input (turn done); blocked → needs_input (waiting on the user); error → needs_input with the error kept for display (process exit still decides done/failed).
- **Trust dialog (no OSC needed for the fallback):** "Do you trust the contents of this project?", options `> Yes, I trust this folder` / `  No, exit`, footer `↑/↓ Navigate · enter Confirm`. Selected marker is `> `. agy remembers trust per folder after the first answer. rex showed the session as **working** while this dialog was up (the row-2 gap).
- **Exit lines:** `Resume with -c (or command below):` then `agy --conversation=<uuid>` — the planned `capture_regex` matches.
- **U6 evidence:** REP (`CSI n b`) ×23, DECST8C ×2, HT ×2 in one short session; with the merged vt10x REP/tab fixes the logo rendered whole and centered in a 166×36 popup, including after detach/re-attach.
- Account note: the agy account hit its quota during the capture, so no model output; status signals were still emitted.
