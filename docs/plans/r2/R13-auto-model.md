# R13 Insert mode picks the model from the prompt (epic)

## Summary
- `i` (FocusPrompt) quick-spawns with the fixed `default_spawn_tool` / `default_spawn_model` / `default_spawn_effort` settings (`update.go:402`, `quickSpawnSlug` `:580`).
- R13 adds an **auto** choice: rex reads the prompt and picks tool, model and effort, shows the pick inline before spawning, and lets the user override with one key.

## Current behavior
- Quick spawn always uses the three defaults. The wizard (`n`) lets the user pick by hand. P1 (round 1) adds name/model/skills to the wizard.
- Ollama is already wired for summaries (`features/summarizer/client.go`, `/api/generate`), optional, local.

## Design
- New flag `auto_model` (Settings → Features). When on, quick spawn routes the model; the tool stays `default_spawn_tool`.
- `internal/features/route`: `Route(ctx, prompt string, avail []registry.Tool) (Choice, Reason, error)`; `Choice{Tool, Model, Effort string}`.
- Router (D1): **Ollama only**. One `/api/generate` call through the existing summarizer client, `format: json`, 1.5 s timeout, prompt truncated to the first 4 KiB, output constrained to one class name (`quick`, `standard`, `deep`). Anything else → `standard`.
- Class → model map, per user (`route_map: {quick: haiku/low, standard: sonnet/medium, deep: opus/high}`), within `default_spawn_tool` (D3), validated against installed models (R02).
- Ollama down, slow or model missing (D4): spawn with `default_spawn_*` and show "auto unavailable: <reason>".
- UX (D2): rex classifies 600 ms after typing stops and shows `→ sonnet · medium (deep)` on the prompt line. `enter` spawns with the shown pick (if a classify is still in flight, it waits up to the 1.5 s cap). `tab` cycles the class by hand. `ctrl+enter` skips routing.
- Logged into the audit record (R06) as `route_reason` so the user can tune `route_map`.

## Feature flag
- `auto_model` (off). Off = quick spawn uses `default_spawn_*` exactly as today, no Ollama call.
- Lives in Settings → Features (roadmap "Feature flags"). Its owning step adds an **off-is-inert** test: with the flag off, no goroutine/subprocess/network/file write happens and the feature's keys and commands answer "off: enable in Settings → Features".
- Flag lands in step R13-2.

## Plan
1. **R13-1 Route package.** `Route` with an injected `Classifier` interface. Tests with a stub classifier: class → model map; model missing from tool → default with notice; empty prompt → standard.
2. **R13-2 Settings.** `auto_model` flag, `route_map`. Validation: every class maps to a model of `default_spawn_tool`.
3. **R13-3 Prompt-line preview + overrides.** Goldens; `tab` cycles; `ctrl+enter` bypasses.
4. **R13-4 Ollama classifier.** Tests with stub HTTP server: timeout → standard; junk output → standard; prompt never logged.
5. **R13-5 Docs + CHANGELOG.**

## Review focus
- Prompt contains secrets: sent only to local Ollama (`OLLAMA_HOST` pointing off-machine gets a one-time warning), and only when `auto_model` is on.
- Ollama slow/down: spawn is never delayed more than the 1.5 s cap.
- Model in `route_map` no longer offered by the tool: fall back with a notice, don't fail the spawn.
- Very long pasted prompt: classifier sees the first 4 KiB only.
- Fast typing: debounce means at most one in-flight classify; stale results (prompt changed since) are dropped.

## Decisions (answered 2026-11-11)

| # | Question | Answer |
|---|---|---|
| D1 | Router | Ollama only (no rules table). |
| D2 | Confirmation | Inline preview after classify; `enter` spawns, `tab` cycles class. |
| D3 | What auto changes | Model + effort within `default_spawn_tool`. |
| D4 | Ollama down/slow | After the 1.5 s cap, spawn with `default_spawn_*` and show "auto unavailable". |
