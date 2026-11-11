# R13 Insert mode picks the model from the prompt (epic)

## Summary
- `i` (FocusPrompt) quick-spawns with the fixed `default_spawn_tool` / `default_spawn_model` / `default_spawn_effort` settings (`update.go:402`, `quickSpawnSlug` `:580`).
- R13 adds an **auto** choice: rex reads the prompt and picks tool, model and effort, shows the pick inline before spawning, and lets the user override with one key.

## Current behavior
- Quick spawn always uses the three defaults. The wizard (`n`) lets the user pick by hand. P1 (round 1) adds name/model/skills to the wizard.
- Ollama is already wired for summaries (`features/summarizer/client.go`, `/api/generate`), optional, local.

## Design
- New setting value `default_spawn_model: auto` (and `default_spawn_tool: auto`).
- `internal/features/route`: `Route(ctx, prompt string, avail []registry.Tool) (Choice, Reason, error)`; `Choice{Tool, Model, Effort string}`.
- Router per Decision D1. Recommended: **rules first, then optional Ollama**:
  - Rules table (YAML/Lua via R05): keyword/regex → class (`quick`, `standard`, `deep`, `research`), plus length thresholds. Example: `^(fix typo|rename|format)` → quick; `(architect|design|refactor .* across|plan)` → deep.
  - Class → model map, per user (`route_map: {quick: claude/haiku/low, standard: claude/sonnet/medium, deep: claude/opus/high}`), only installed tools (R02).
  - If no rule hits and `route_llm` is on and Ollama is reachable: one classification call with a 1.5 s timeout, output constrained to one of the class names; anything else → `standard`.
- UX: prompt line shows `→ sonnet · medium (rule: "refactor")` as you type (debounced 150 ms, rules only; LLM runs on enter). `enter` spawns; `tab` cycles classes; `ctrl+enter` spawns with defaults (no routing).
- Logged into the audit record (R06) as `route_reason` so the user can tune rules.

## Plan
1. **R13-1 Route package, rules only.** Tests: table of prompts → class; unavailable tool in map → next installed fallback; empty prompt → standard.
2. **R13-2 Settings.** `auto` option, `route_map`, `route_rules`, `route_llm`. Validation: every class maps to an existing tool/model.
3. **R13-3 Prompt-line preview + overrides.** Goldens; `tab` cycles; `ctrl+enter` bypasses.
4. **R13-4 Ollama classifier (if D1 = a/b).** Tests with stub HTTP server: timeout → standard; junk output → standard; prompt never logged.
5. **R13-5 Docs + CHANGELOG.**

## Review focus
- Prompt contains secrets: never sent anywhere except local Ollama, and only when `route_llm` is on.
- Ollama slow/down: spawn is never delayed more than the 1.5 s cap.
- Routed model removed from the tool since rules were written: fall back with a notice, don't fail the spawn.
- Very long pasted prompt: rules run on the first 4 KiB only.

## Decisions

| # | Question | Options (recommended first) |
|---|---|---|
| D1 | Router | (a) rules first, local Ollama classifier as optional fallback · (b) Ollama only · (c) rules only |
| D2 | Confirmation | (a) inline preview, `enter` spawns immediately · (b) always confirm the pick with a second `enter` · (c) silent: just spawn |
| D3 | What auto may change | (a) model + effort within the default tool · (b) tool too (e.g. claude ↔ agy) · (c) model only |
