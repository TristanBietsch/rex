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

1. **R02-1 Live captures.** For qwen, cursor-agent and grok: run in a temp folder with a one-line prompt under `dev/sandbox.sh`, save raw PTY bytes to the scratchpad, record idle footer, busy footer, trust dialog, permission prompt. Output: a table in this file (not the bytes). No code.
2. **R02-2 Registry fields.** `types.go`: `ModelsCmd *ModelsCmd`, `ModelArgTemplate []string`. Loader validation (args non-empty, parse ∈ {lines,json}, timeout 100–30000 ms). Tests: `TestLoaderModelsCmdValidation` table (missing args, bad parse, json without path).
3. **R02-3 Builtin entries.** Three tools in `builtin.yaml` with regexes from R02-1. Tests: `TestBuiltinNewToolsDetect` feeding sanitized synthetic fixtures (written by hand to match captured shapes) through the heuristic adapter: idle → needs_input, busy → working.
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
| D4 | State detection | Heuristic footer regexes now; hook adapter for qwen as a follow-up. |
