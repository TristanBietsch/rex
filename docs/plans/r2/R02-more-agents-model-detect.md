# R02 More agents (Qwen Code, Cursor CLI, Grok Build) and model auto-detect

## Summary
- Add three tools to `builtin.yaml`: `qwen` (Qwen Code), `cursor` (Cursor CLI, binary `cursor-agent`), and replace the stale `grok` entry with Grok Build (`grok` 1.0.x). oh-my-pi (`omp`) is out (D1).
- Settings and the spawn wizard show only tools whose binary is on `PATH`, and each tool's model list comes from the tool's own model-listing command when it has one, cached by the daemon, with `builtin.yaml` models as the fallback.
- Builds on round 1: U3-1 (retired-tool guard; the rest of U3 was dropped by D13, so codex/ollama/deepseek/kimi stay in the registry), the T3/A1 `spawnSpec`, A1's `OutputObserver`, and P1's wizard fields.

## Current behavior
- Tools are static YAML (`internal/catalog/registry/builtin.yaml`), merged with `~/.config/rex/tools.yaml` (`loader.go`). Model lists are hard-coded and already stale (e.g. claude "Opus 4.7", grok "grok-4").
- `enabled_by_default: false` hides a tool from the wizard until enabled in Settings → Onboarding. Nothing checks whether the binary exists; a missing binary fails at spawn time.
- State detection is `kind: hooks` (claude) or `kind: heuristic` (`prompt_regex`, `busy_regex`, `idle_ms`).
- U3 was dropped in round 1 (D13): no tools were removed.

### What the CLIs offer (checked on this machine, 2026-11-11)

| Tool | Binary / version | Model flag | Model list | Resume | Skip approvals | Hooks |
|---|---|---|---|---|---|---|
| Qwen Code | `qwen` 1.0.0-preview | `-m, --model` | `--list-models` | `-r, --resume [id]`, `--list-sessions` | `--yolo` (verify) | `qwen hooks` subcommand |
| Cursor CLI | `cursor-agent` 2026.11.10 | `--model` | `cursor-agent models` / `--list-models` | `--resume [chatId]`, `ls` | `--yolo` / `--force` | none found |
| Grok Build | `grok` 1.0.77 | `-m, --model` | `grok models` | `-r, --resume [id]` | (verify) | none found; has `grok usage` (R10) |

All are full-screen TUIs, so they use the attach `repaint` path like agy.

## R02-1 capture results (2026-11-11)

Method: each CLI in a private tmux server (`tmux -L rexcap`, 120×40, `TERM=tmux-256color`), fresh temp git folder, tiny prompts ("Reply with the single word: pong"; "Create a file named hello.txt containing the word hi", rejected with Esc). Raw bytes kept in the session scratchpad only, never committed. Footer text is matched on the rendered screen; it never appears as plain text in the raw bytes (words are split by cursor moves and colors).

### qwen (Qwen Code 1.0.0-preview): done

| What | Finding |
|---|---|
| Trust dialog | "Do you trust the files in this folder?" → `❯ 1. Trust folder` / `2. Don't trust and exit` (choosing 1 writes qwen's global trusted list) |
| Session-only trust | `--settings '{"security":{"folderTrust":{"enabled":false}}}'` skips the dialog for that run only; nothing written. Becomes `trust_args`, added only when the cwd is in `auto_trust_dirs` (same rule as agy) |
| Idle screen | `? for shortcuts` (right of the input), `> Type your message or @path/to/file` |
| Busy screen | `⠇ Generating... (esc to cancel, 1s)`; `? for shortcuts` **stays visible while busy** (same as agy), so `busy_regex: "esc to cancel"` is required |
| Permission prompt | "Permission Required" … "Apply this change?" → `1. Allow once` … `5. No`; neither footer string is on screen then |
| **Status channel** | qwen speaks **OSC 7501** (the channel A1 found in agy). Turned on per session with env **`QWEN_PROGRAM_STATUS=1`** (`0` forces off; otherwise qwen asks the terminal with `ESC]7501;?`). Format: `ESC]7501;state=<s>[:id=…][:kind=…][:progress=N]:app=qwen[:title=<b64>][:msg=<b64>]ESC\` |
| States seen | `idle` → `working` → `done`; on the permission prompt `blocked` with `kind=permission`, `msg="Tool confirmation required"`. Source also has `kind=question`, `kind=auth`, and `state=error` |
| Window title | OSC 0 `◇ <task> \| Ready` / `✦ <task> \| Working`; stays "Working" during a permission prompt, so not enough on its own |
| Initial prompt | `-i, --prompt-interactive <text>` (a bare positional prompt is one-shot) |
| Models | `--list-models`: header `MODEL`, then `Name (id)` per line, e.g. `Qwen3.8-Flash (qfmodel)`. `-m` accepts both the name and the id; use the id |
| Other flags | `--session-id <id>` (preassign, like Claude; for T3 resume), `-r/--resume [id]`, `--plugin-dir <dir>` (P1 skills work as for Claude), `--dangerously-skip-permissions` (K1 `skip_permissions_args`), `--reasoning-effort <level>` |

### cursor-agent (2026.11.10-731fb78): blocked, not signed in
- Without a login it shows "Press any key to log in..." and nothing else. Treat that screen as `needs_input` with the notice "cursor: not signed in — run `cursor-agent login`".
- No OSC 7501 check seen on that screen. Rest of the capture waits for the user to sign in.

### grok (Grok Build 1.0.77): done

| What | Finding |
|---|---|
| Signed out | browser device-code sign-in: "Approve in your browser to finish signing in." … "Waiting for approval..." · `ctrl+q quit` |
| Trust dialog | "Do you trust the contents of this directory?" → `Yes, proceed  y` / `No, quit  n`. **No session-only option**: `y` and `--trust` both write `~/.grok/trusted_folders.toml`. So rex cannot auto-trust grok without a global write; the user answers it (or rex answers `y` only with an explicit per-tool opt-in, see D5) |
| Idle footer | `Shift+Tab:mode │ Ctrl+x:shortcuts` |
| Busy footer | adds `Ctrl+c:cancel` (and `Ctrl+b:send to bg` while a tool runs); spinner line `⠸ Running …` |
| Permissions | the default mode **did not ask**: in a trusted folder grok wrote `hello.txt` and ran a command without a prompt. Its docs list a `blocked` state with `kind` permission / question / auth for when it does ask |
| **Status channel** | OSC 7501, setting `[ui.notifications] program_status = auto\|on\|off` (default `auto`). Under `auto` grok sends the support query `ESC]7501;?` only when it is **not** inside tmux and the terminal is unrecognized; inside tmux it stays silent. The `GROK_CONFIG` env overlay cannot set it (overlay allowlist is `models`, `features`, `toolset`, `shell_environment_policy`) |
| States seen (query answered) | `idle` → `working` (`msg`: "Waiting for response…", "Thinking", "Responding") → `done` (`msg`: "Turn complete in 9.5s."); `app=grok` |
| Initial prompt | positional `[PROMPT]` starts an **interactive** session with that prompt (`-p/--single` is one-shot) |
| Models | `grok models`: "Available models:" then `  * grok-4.7 (default)` per line |
| Other flags | `-m/--model`, `--reasoning-effort`/`--effort`, `-r/--resume [id]`, `--permission-mode default\|acceptEdits\|auto\|dontAsk\|bypassPermissions\|plan`, `--always-approve` (K1), `--cwd` |
| Other screens | a "Help improve Grok … [Opt out] [Opt in]" data-sharing banner on first runs (off by default; rex never answers it) |

### OSC 7501: one mechanism for qwen, grok and agy
- The protocol is public: "Program Status Protocol (OSC 7501)", superlogical.com/rex/docs/build/program-status (a terminal that is also called Rex; unrelated to this project, same name). A terminal that supports it answers the program's query `OSC 7501 ; ? ST` with `OSC 7501 ; ? : states=idle,working,done,blocked,error ST`; the program then sends `OSC 7501 ; state=<s>[:id=…][:kind=permission|question|auth][:progress=N][:app=…][:title=<b64>][:msg=<b64>] ST`, and `state=clear` on exit. Terminators: `ESC \` or BEL.
- **Verified 2026-11-11** with a fake terminal (`osc7501_term.py` in the session scratchpad) that answers only that query (plus DA1 and XTVERSION): **grok and qwen both started reporting, with no env var and no config change.** agy (A1) uses the same protocol.
- Two conditions in the child environment:
  1. rex must answer the query on the PTY (the daemon already reads every byte; the vt10x screen mirror does not answer it today);
  2. `TMUX`, `TMUX_PANE`, `TERM_PROGRAM`, `TERM_PROGRAM_VERSION` must be removed from the child env, or grok thinks it runs inside tmux and never asks. The agent does run inside rex's PTY, not tmux, so removing them is correct anyway.
- This replaces per-tool tricks (`QWEN_PROGRAM_STATUS=1`, A1's `AGY_CLI_OSC7501=on`) and screen regexes as the primary state source. Footer regexes stay as the fallback for tools or versions that don't speak it.
- **Note for round 1 (A1):** A1-2's `OutputObserver` could answer the query the same way instead of (or in addition to) setting `AGY_CLI_OSC7501=on`. This is a suggestion for the round-1 orchestrator, not a change to the round-1 plan.

### Design changes from the captures
- **State from OSC 7501 for every tool that speaks it.** New registry `detect.kind: osc7501` (with the existing footer regexes as fallback fields). The daemon (a) answers `ESC]7501;?` on the child PTY with the states reply, (b) strips `TMUX*`/`TERM_PROGRAM*` from the child env, (c) parses reports in the PTY reader (A1's `OutputObserver` slot). Map: `working` → working; `blocked` → needs_input with `wait_reason` = kind; `done` → needs_input (S4 "ready for review"); `idle` → needs_input; `error` → needs_input with the decoded `msg`; `clear` → no change (exit is handled by the process). Child records (`id=a/b`) are ignored at first; a later step can feed S4/T-subagent counts.
- **Fallback regexes**
  - qwen: `prompt_regex: "\\? for shortcuts|Apply this change\\?|Do you trust the files in this folder\\?"`, `busy_regex: "esc to cancel"`.
  - grok: `prompt_regex: "Shift\\+Tab:mode|Do you trust the contents of this directory\\?"`, `busy_regex: "Ctrl\\+c:cancel"`.
- **Per-tool prompt delivery** in the registry, replacing the tool-id switch in `spawn_prompt.go`: `prompt_args` (qwen `["-i", "{prompt}"]`, grok `["{prompt}"]` after `--`).
- **`models_cmd` parsing** gets `id_regex` and `skip_header`: qwen `--list-models`, id `\\(([^)]+)\\)\\s*$`; grok `models`, id `^\\s*\\*\\s+(\\S+)`.
- **Signed-out screens** become a per-tool `login_regex` (cursor "Press any key to log in", grok "Approve in your browser to finish signing in") → needs_input with the fixed notice "<tool>: not signed in — run `<cmd> login`".
- **Trust**: qwen has a session-only switch (`trust_args`); grok has none, so rex leaves grok's dialog to the user (D5).
- **grok permissions**: its default mode can edit files and run commands without asking. Document it in `docs/registry.md`; rex does not change grok's mode (that is the user's grok config).

## Design

### 1. Registry entries
One entry per tool with `command`, `detect` (heuristic until a hooks adapter exists), `skip_permissions_args` (K1), `trust_args` where the CLI has one (`cursor-agent --trust`), and a new optional `models_cmd` block:

```yaml
models_cmd:
  args: ["--list-models"]      # appended to command
  parse: lines | json          # lines: one id per line, json: JSONPath-lite "models[].id"
  json_path: "models[].id"     # only for parse: json
  timeout_ms: 5000
```

Detect regexes come from live captures (step R02-1), not guesses. Captures stay in the session scratchpad, never committed (same rule as A1/D2).

### 2. Installed-tool detection
- `registry.Installed(tool) bool`: `exec.LookPath(tool.Command[0])`. Computed by the daemon at load and on SIGHUP/`rex reload`; sent as `installed` on the tool list the TUI already receives.
- Wizard shows installed tools only; Settings → Onboarding lists uninstalled ones dimmed with "not on PATH" and an install hint (D2).

### 3. Model auto-detect
- New `internal/catalog/registry/models.go`: `DetectModels(ctx, tool) ([]Model, error)` runs `models_cmd` with a timeout, no stdin, `HOME` unchanged, output capped at 64 KiB; parses ids; maps each to `Model{ID, Name: ID, Args: tool.model_arg_template}`.
- New per-tool `model_arg_template` (e.g. `["--model", "{model}"]`) so a detected id becomes argv without a hand-written `args` entry.
- Cache: `~/.local/state/rex/models-cache.json` keyed by tool id + binary mtime; refreshed per Decision D3.
- Merge: detected models first, then any `builtin.yaml`/`tools.yaml` models not in the detected set (keeps effort metadata). A failed detect logs once and falls back to static models.
- Security: the command is fixed in the registry (user-editable only via their own `tools.yaml`), never built from session output. Detected ids are validated against `^[A-Za-z0-9._:/@\[\]-]{1,128}$` before reaching argv.

### 4. Interaction with U3
U3 was dropped (D13), so no tool was removed. R02 adds qwen and cursor and replaces the stale `grok` entry, all `enabled_by_default: true` and gated by `Installed`. The other existing entries (codex, ollama, deepseek, kimi, hidden gemini) keep today's settings, still gated by `Installed`; omp is not added (D1).

## Feature flag
- `extra_agents` (on): qwen, cursor, grok entries; `model_autodetect` (on): run `models_cmd`. Off = static `builtin.yaml` models, no subprocess.
- Lives in Settings → Features (roadmap "Feature flags"). Its owning step adds an **off-is-inert** test: with the flag off, no goroutine/subprocess/network/file write happens and the feature's keys and commands answer "off: enable in Settings → Features".
- Flag lands in step R02-2.

## Plan (tasks; each = one commit, test first, `make check`)

1. **R02-1 Live captures.** qwen and grok done (above); cursor-agent waits for sign-in. For each: run in a temp folder with a one-line prompt under `dev/sandbox.sh`, save raw PTY bytes to the scratchpad, record idle footer, busy footer, trust dialog, permission prompt. Output: a table in this file (not the bytes). No code.
2. **R02-2a OSC 7501 terminal side (D6).** `internal/daemon/adapter/osc7501.go`: `Parse(b []byte) []Report` (stateful across chunk boundaries), `QueryReply` constant; supervisor writes the reply when the query appears; `childEnvFor` drops `TMUX`, `TMUX_PANE`, `TERM_PROGRAM`, `TERM_PROGRAM_VERSION`; registry `detect.kind: osc7501` with regex fallback fields. Tests: query split across two reads still answered once; reports with BEL and ST terminators; base64 `msg` decoded and sanitized; `blocked` sets `wait_reason`; unknown state ignored; a tool that never reports falls back to its regexes after the first prompt match; fuzz `FuzzParse`. Coordinate with A1-2 (same reader slot) so there is one parser.
3. **R02-2 Registry fields.** `types.go`: `ModelsCmd *ModelsCmd` (`args`, `parse`, `json_path`, `id_regex`, `skip_header`, `timeout_ms`), `ModelArgTemplate []string`, `PromptArgs []string` (`{prompt}`; replaces the tool-id switch in `spawn_prompt.go`), `LoginRegex string`. Loader validation (args non-empty, parse ∈ {lines,json}, regexes compile, timeout 100–30000 ms). Tests: `TestLoaderModelsCmdValidation` table; qwen and grok model-list fixtures (synthetic, shaped like the captures) parse to `qfmodel…` / `grok-4.7`; login screen → needs_input with the notice.
3. **R02-3 Builtin entries.** Three tools in `builtin.yaml` (`detect.kind: osc7501`, regex fallbacks, `prompt_args`, `models_cmd`, `login_regex`, qwen `trust_args`). Tests: `TestBuiltinNewToolsDetect` feeding sanitized synthetic fixtures (written by hand to match captured shapes) through the heuristic adapter: idle → needs_input, busy → working.
4. **R02-4 Installed.** `registry.Installed`, `installed` field on the wire tool list (additive). Wizard hides uninstalled tools; onboarding shows them dimmed. Tests: fake `PATH` in `t.TempDir()` with a stub script; Windows uses `.bat` stub or skips.
5. **R02-5 DetectModels + cache.** Tests: stub binary printing lines / JSON / garbage / sleeping past timeout / 1 MiB output; id validator table; cache invalidated by mtime change.
6. **R02-6 Merge into wizard + settings model pickers.** `default_spawn_model` options become dynamic. Tests: wizard snapshot with detected + static models; detect failure shows static list and one notice.
7. **R02-7 Docs.** `docs/registry.md` (`models_cmd`, `model_arg_template`), `docs/settings.md`, CHANGELOG.

## Review focus
- A tool whose `--list-models` needs login prints an auth error on stdout: must parse to zero models and fall back, not show "Please log in" as a model.
- Binary on `PATH` is a shim (asdf/nvm) whose mtime never changes: cache must also expire by age.
- Model id containing spaces or shell metacharacters from a hostile `tools.yaml`: rejected by validator.
- Wizard opened while detect is still running: shows static models, updates in place.
- Windows: `LookPath` needs `PATHEXT`; tests cover `.exe`/`.cmd`.

## Decisions (answered 2026-11-11)

| # | Question | Answer |
|---|---|---|
| D1 | Visible tools | claude, antigravity, qwen, cursor, grok, each gated on installed. **No omp.** (U3 dropped by D13: other existing tools keep their current defaults.) |
| D2 | Not-installed tools | Hidden in wizard; Settings → Onboarding lists them dimmed with an install hint. |
| D3 | Model refresh | Daemon start + `rex reload` + 24 h age, cached on disk. |
| D4 | State detection | Superseded by D6 (OSC 7501 primary, footer regexes as fallback). No hook adapters needed for qwen or grok. |
| D5 | grok folder trust | rex never answers grok's trust dialog and never passes `--trust`; the session shows "needs you" until the user presses `y` once per folder. `auto_trust_dirs` does not apply to grok (documented). |
| D6 | OSC 7501 | Primary state source: the daemon answers `ESC]7501;?` on the child PTY and strips `TMUX*`/`TERM_PROGRAM*` from the child env; footer regexes are the fallback. |
